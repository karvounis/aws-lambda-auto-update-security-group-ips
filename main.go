package main

import (
	"errors"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"go.uber.org/zap"
	"os"
	"time"
)

// IncomingEvent is the event that CloudWatch triggers
type IncomingEvent struct {
	Version    string    `json:"version"`
	ID         string    `json:"id"`
	DetailType string    `json:"detail-type"`
	Source     string    `json:"source"`
	AccountID  string    `json:"account"`
	Region     string    `json:"region"`
	Resources  []string  `json:"resources"`
	Detail     Detail    `json:"detail"`
	Time       time.Time `json:"time"`
}

// Detail contain the details of the EC2 lifecycle hook
type Detail struct {
	LifecycleHookName    string `json:"LifecycleHookName"`
	AutoScalingGroupName string `json:"AutoScalingGroupName"`
	LifecycleActionToken string `json:"LifecycleActionToken"`
	LifecycleTransition  string `json:"LifecycleTransition"`
	EC2InstanceID        string `json:"EC2InstanceId"`
}

// Response returns the list of IPs that were added and removed
type Response struct {
	AddedIPs   []string `json:"added_ips"`
	RemovedIPs []string `json:"removed_ips"`
}

// HTTPSPort is the port 443
const HTTPSPort = 443

// TCPProtocol specifies the tcp protocol
const TCPProtocol = "tcp"

// LifecycleActionResultContinue the continue action for the group to take
const LifecycleActionResultContinue = "CONTINUE"

// LifecycleActionResultAbandon the abandon action for the group to take
const LifecycleActionResultAbandon = "ABANDON"

func main() {
	lambda.Start(Handler)
}

// Handler Automatically update (add/remove) a specific security group's rules based on the public IPs of an autoscaling group's managed EC2 instances.
// This lambda function is initiated by AutoScaling Lifecycle Hooks.
func Handler(request IncomingEvent) (response Response, err error) {
	logger, _ := zap.NewProduction()
	defer logger.Sync()
	logger.Info("IncomingEvent", zap.Any("Request", request))

	sess, err := session.NewSession(&aws.Config{Region: aws.String(request.Region)})
	if err != nil {
		logger.Error("Failed to create session", zap.Error(err))
		return response, err
	}

	ec2Svc := ec2.New(sess)
	autoscalingSvc := autoscaling.New(sess)
	asgIPs, err := getASGPublicIPs(request, autoscalingSvc, ec2Svc)
	if err != nil {
		logger.Error("Failed to get ASG Public IPs", zap.Error(err))
		sendResponseToASG(autoscalingSvc, request, LifecycleActionResultAbandon)
		return response, err
	}
	logger.Info("AutoScaling Group's IPs", zap.Any("asgIPs", asgIPs))

	sgID := os.Getenv("securityGroupID")
	sgIPs, err := getSGIPs(sgID, ec2Svc)
	if err != nil {
		logger.Error("Failed to get the IPs of the Security Groups", zap.Error(err))
		sendResponseToASG(autoscalingSvc, request, LifecycleActionResultAbandon)
		return response, err
	}
	logger.Info("Security Group's IPs", zap.Any("sgIPs", sgIPs))

	ipsToAdd := getIPsToAdd(asgIPs, sgIPs)
	logger.Info("IPs to add", zap.Any("ipsToAdd", ipsToAdd))

	ipsToRemove := getIPsToRemove(sgIPs, asgIPs)
	logger.Info("IPs to remove", zap.Any("ipsToRemove", ipsToRemove))

	if len(ipsToAdd) != 0 {
		var addPermissions []*ec2.IpPermission
		for _, ip := range ipsToAdd {
			addPermissions = append(addPermissions, &ec2.IpPermission{
				FromPort:   aws.Int64(HTTPSPort),
				ToPort:     aws.Int64(HTTPSPort),
				IpRanges:   []*ec2.IpRange{{CidrIp: aws.String(ip)}},
				IpProtocol: aws.String(TCPProtocol),
			})
		}

		_, err := ec2Svc.AuthorizeSecurityGroupIngress(&ec2.AuthorizeSecurityGroupIngressInput{
			GroupId:       aws.String(sgID),
			IpPermissions: addPermissions,
		})
		if err != nil {
			logger.Error("Failed to add IPs to security group", zap.Error(err))
			sendResponseToASG(autoscalingSvc, request, LifecycleActionResultAbandon)
			return response, err
		}
	}

	if len(ipsToRemove) != 0 {
		var removePermissions []*ec2.IpPermission
		for _, v := range ipsToRemove {
			removePermissions = append(removePermissions, &ec2.IpPermission{
				FromPort:   aws.Int64(HTTPSPort),
				ToPort:     aws.Int64(HTTPSPort),
				IpRanges:   []*ec2.IpRange{{CidrIp: aws.String(v)}},
				IpProtocol: aws.String(TCPProtocol),
			})
		}

		_, err := ec2Svc.RevokeSecurityGroupIngress(&ec2.RevokeSecurityGroupIngressInput{
			GroupId:       aws.String(sgID),
			IpPermissions: removePermissions,
		})
		if err != nil {
			logger.Error("Failed to remove IPs from security group", zap.Error(err))
			sendResponseToASG(autoscalingSvc, request, LifecycleActionResultAbandon)
			return response, err
		}
	}

	sendResponseToASG(autoscalingSvc, request, LifecycleActionResultContinue)
	return Response{AddedIPs: ipsToAdd, RemovedIPs: ipsToRemove}, err
}

// Completes the lifecycle action for the specified token or instance with the specified result.
func sendResponseToASG(autoscalingSvc *autoscaling.AutoScaling, request IncomingEvent, status string) {
	autoscalingSvc.CompleteLifecycleAction(&autoscaling.CompleteLifecycleActionInput{
		AutoScalingGroupName:  aws.String(request.Detail.AutoScalingGroupName),
		InstanceId:            aws.String(request.Detail.EC2InstanceID),
		LifecycleActionResult: aws.String(status),
		LifecycleActionToken:  aws.String(request.Detail.LifecycleActionToken),
		LifecycleHookName:     aws.String(request.Detail.LifecycleHookName),
	})
}

// Calculates which AutoScaling Group IPs cannot be found in the Security Group IPs. These ones will be added to SG.
func getIPsToAdd(asgIPs map[string]string, sgIPs map[string]string) (ipsToAdd []string) {
	for i := range asgIPs {
		if _, ok := sgIPs[i]; !ok {
			ipsToAdd = append(ipsToAdd, i)
		}
	}
	return ipsToAdd
}

// Calculates which Security Group IPs cannot be found in the AutoScaling Group IPs. These ones will be removed from SG.
func getIPsToRemove(sgIPs map[string]string, asgIPs map[string]string) (ipsToRemove []string) {
	for i := range sgIPs {
		if _, ok := asgIPs[i]; !ok {
			ipsToRemove = append(ipsToRemove, i)
		}
	}
	return ipsToRemove
}

// Gets a map of the IPs that are already present in the Security Group
func getSGIPs(sgID string, ec2Svc *ec2.EC2) (map[string]string, error) {
	sgIPs := make(map[string]string)
	sgResp, err := ec2Svc.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		GroupIds: []*string{
			aws.String(sgID),
		},
	})
	if err != nil {
		return sgIPs, err
	}

	if len(sgResp.SecurityGroups[0].IpPermissions) != 0 {
		for _, ipRange := range sgResp.SecurityGroups[0].IpPermissions[0].IpRanges {
			sgIPs[aws.StringValue(ipRange.CidrIp)] = aws.StringValue(ipRange.CidrIp)
		}
	}
	return sgIPs, err
}

// Gets a map of running public IPs for all instances of the Autoscaling Group
func getASGPublicIPs(event IncomingEvent, autoscalingSvc *autoscaling.AutoScaling, ec2Svc *ec2.EC2) (map[string]string, error) {
	ips := make(map[string]string)
	asgResp, err := autoscalingSvc.DescribeAutoScalingGroups(&autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{aws.String(event.Detail.AutoScalingGroupName)},
	})
	if err != nil {
		return ips, err
	}
	if asgResp.String() == "{\n\n}" {
		return ips, errors.New("autoscaling group response is empty")
	}

	for _, instance := range asgResp.AutoScalingGroups[0].Instances {
		ec2Response, err := ec2Svc.DescribeInstances(&ec2.DescribeInstancesInput{
			InstanceIds: []*string{instance.InstanceId},
		})
		if err != nil {
			return ips, err
		}

		for _, rsv := range ec2Response.Reservations {
			rsvInst := rsv.Instances[0]
			if event.Detail.LifecycleTransition == "autoscaling:EC2_INSTANCE_TERMINATING" && aws.StringValue(rsvInst.InstanceId) == event.Detail.EC2InstanceID {
				continue
			}
			if aws.StringValue(rsvInst.State.Name) != "shutting-down" && aws.StringValue(rsvInst.State.Name) != "terminated" && aws.StringValue(rsvInst.PublicIpAddress) != "" {
				ips[aws.StringValue(rsvInst.PublicIpAddress)+"/32"] = aws.StringValue(rsvInst.PublicIpAddress)
			}
		}
	}
	return ips, err
}
