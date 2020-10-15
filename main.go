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

type Detail struct {
	LifecycleHookName    string `json:"LifecycleHookName"`
	AutoScalingGroupName string `json:"AutoScalingGroupName"`
	LifecycleActionToken string `json:"LifecycleActionToken"`
	LifecycleTransition  string `json:"LifecycleTransition"`
	EC2InstanceId        string `json:"EC2InstanceId"`
}

type Response struct {
	AddedIPs   []string `json:"added_ips"`
	RemovedIPs []string `json:"removed_ips"`
}

func main() {
	lambda.Start(Handler)
}

func Handler(request IncomingEvent) (response Response, err error) {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	sess, err := session.NewSession(&aws.Config{Region: aws.String(request.Region)})
	if err != nil {
		logger.Error("Failed to create session", zap.Error(err))
		return response, err
	}

	ec2Svc := ec2.New(sess)
	asgIPs, err := getASGPublicIPs(request, autoscaling.New(sess), ec2Svc)
	if err != nil {
		logger.Error("Failed to get ASG Public IPs", zap.Error(err))
		return response, err
	}
	logger.Info("AutoScaling Group's IPs", zap.Any("asgIPs", asgIPs))

	sgID := os.Getenv("securityGroupID")
	sgIPs, err := getSGIPs(sgID, ec2Svc)
	if err != nil {
		logger.Error("Failed to get the IPs of the Security Groups", zap.Error(err))
		return response, err
	}
	logger.Info("Security Group's IPs", zap.Any("sgIPs", sgIPs))

	ipsToAdd := getIPsToAdd(asgIPs, sgIPs)
	logger.Info("IPs to add", zap.Any("ipsToAdd", ipsToAdd))

	ipsToRemove := getIPsToRemove(asgIPs, sgIPs)
	logger.Info("IPs to remove", zap.Any("ipsToRemove", ipsToRemove))

	if len(ipsToAdd) != 0 {
		var addPermissions []*ec2.IpPermission
		for _, ip := range ipsToAdd {
			addPermissions = append(addPermissions, &ec2.IpPermission{
				FromPort:   aws.Int64(0),
				ToPort:     aws.Int64(65535),
				IpRanges:   []*ec2.IpRange{{CidrIp: aws.String(ip)}},
				IpProtocol: aws.String("tcp"),
			})
		}

		_, err := ec2Svc.AuthorizeSecurityGroupIngress(&ec2.AuthorizeSecurityGroupIngressInput{
			GroupId:       aws.String(sgID),
			IpPermissions: addPermissions,
		})
		if err != nil {
			logger.Error("Failed to add IPs to security group", zap.Error(err))
			return response, err
		}
	}

	if len(ipsToRemove) != 0 {
		var removePermissions []*ec2.IpPermission
		for _, v := range ipsToRemove {
			removePermissions = append(removePermissions, &ec2.IpPermission{
				FromPort:   aws.Int64(0),
				ToPort:     aws.Int64(65535),
				IpRanges:   []*ec2.IpRange{{CidrIp: aws.String(v)}},
				IpProtocol: aws.String("tcp"),
			})
		}

		_, err := ec2Svc.RevokeSecurityGroupIngress(&ec2.RevokeSecurityGroupIngressInput{
			GroupId:       aws.String(sgID),
			IpPermissions: removePermissions,
		})
		if err != nil {
			logger.Error("Failed to remove IPs from security group", zap.Error(err))
			return response, err
		}
	}

	return Response{AddedIPs: ipsToAdd, RemovedIPs: ipsToRemove}, err
}

func getIPsToAdd(asgIPs map[string]string, sgIPs map[string]string) (ipsToAdd []string) {
	for i, _ := range asgIPs {
		if _, ok := sgIPs[i]; !ok {
			ipsToAdd = append(ipsToAdd, i)
		}
	}
	return ipsToAdd
}

func getIPsToRemove(asgIPs map[string]string, sgIPs map[string]string) (ipsToRemove []string) {
	for i, _ := range sgIPs {
		if _, ok := asgIPs[i]; !ok {
			ipsToRemove = append(ipsToRemove, i)
		}
	}
	return ipsToRemove
}

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
			if aws.StringValue(rsvInst.State.Name) != "shutting-down" && aws.StringValue(rsvInst.State.Name) != "terminated" && aws.StringValue(rsvInst.PublicIpAddress) != "" {
				ips[aws.StringValue(rsvInst.PublicIpAddress)+"/32"] = aws.StringValue(rsvInst.PublicIpAddress)
			}
		}
	}
	return ips, err
}
