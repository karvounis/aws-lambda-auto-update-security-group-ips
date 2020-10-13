package main

import (
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"go.uber.org/zap"
	"log"
	"os"
)

type Request struct {
	ID    float64 `json:"id"`
	Value string  `json:"value"`
}

func Handler(request Request) (response string, err error) {
	logger, _ := zap.NewProduction()
	defer logger.Sync()

	logger.Info("Hello there stranger!")

	asgName := os.Getenv("asgName")
	sgID := os.Getenv("sgID")
	region := os.Getenv("region")

	sess, err := session.NewSession(&aws.Config{Region: aws.String(region)})
	if err != nil {
		logger.Error("Failed to create session", zap.Error(err))
		return response, err
	}
	ec2Svc := ec2.New(sess)
	autoscalingSvc := autoscaling.New(sess)

	asgIPs, err := getASGPublicIPs(request, asgName, autoscalingSvc, ec2Svc)
	if err != nil {
		logger.Error("Failed to get ASG Public IPs", zap.Error(err))
		return response, err
	}
	logger.Info("AutoScaling Group's IPs", zap.Any("asgIPs", asgIPs))

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
		var addParams []*ec2.IpPermission
		for _, ip := range ipsToAdd {
			addParams = append(addParams, &ec2.IpPermission{
				FromPort:   aws.Int64(0),
				ToPort:     aws.Int64(65535),
				IpRanges:   []*ec2.IpRange{{CidrIp: aws.String(ip)}},
				IpProtocol: aws.String("tcp"),
			})
		}
		logger.Info("IPs to add request", zap.Any("addParams", addParams))
		_, err := ec2Svc.AuthorizeSecurityGroupIngress(&ec2.AuthorizeSecurityGroupIngressInput{
			GroupId:       aws.String(sgID),
			IpPermissions: addParams,
		})
		if err != nil {
			logger.Error("Failed to add IPs to security group", zap.Error(err))
			return response, err
		}
	}

	if len(ipsToRemove) != 0 {
		var removeParams []*ec2.IpPermission
		for _, v := range ipsToRemove {
			removeParams = append(removeParams, &ec2.IpPermission{
				FromPort:   aws.Int64(0),
				ToPort:     aws.Int64(65535),
				IpRanges:   []*ec2.IpRange{{CidrIp: aws.String(v)}},
				IpProtocol: aws.String("tcp"),
			})
		}
		_, err := ec2Svc.RevokeSecurityGroupIngress(&ec2.RevokeSecurityGroupIngressInput{
			GroupId:       aws.String(sgID),
			IpPermissions: removeParams,
		})
		if err != nil {
			logger.Error("Failed to remove IPs from security group", zap.Error(err))
			return response, err
		}
	}

	return "All ok!", err
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
	sgResponse, err := ec2Svc.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		GroupIds: []*string{
			aws.String(sgID),
		},
	})
	if err != nil {
		return sgIPs, err
	}

	if len(sgResponse.SecurityGroups[0].IpPermissions) != 0 {
		for _, ipRange := range sgResponse.SecurityGroups[0].IpPermissions[0].IpRanges {
			sgIPs[aws.StringValue(ipRange.CidrIp)] = aws.StringValue(ipRange.CidrIp)
		}
	}
	return sgIPs, err
}

func getASGPublicIPs(event Request, asgName string, autoscalingSvc *autoscaling.AutoScaling, ec2Svc *ec2.EC2) (map[string]string, error) {
	ips := make(map[string]string)
	asgResponse, err := autoscalingSvc.DescribeAutoScalingGroups(&autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{aws.String(asgName)},
	})
	if err != nil {
		return ips, err
	}
	if asgResponse.String() == "{\n\n}" {
		return ips, errors.New("autoscaling group response is empty")
	}
	if len(asgResponse.AutoScalingGroups[0].Instances) == 0 {
		return ips, err
	}

	for _, instance := range asgResponse.AutoScalingGroups[0].Instances {
		ec2Response, err := ec2Svc.DescribeInstances(&ec2.DescribeInstancesInput{
			InstanceIds: []*string{instance.InstanceId},
		})
		if err != nil {
			return ips, err
		}
		for _, reservation := range ec2Response.Reservations {
			if *reservation.Instances[0].State.Name != "shutting-down" && *reservation.Instances[0].State.Name != "terminated" {
				ips[aws.StringValue(reservation.Instances[0].PublicIpAddress)+"/32"] = aws.StringValue(reservation.Instances[0].PublicIpAddress)
			}
		}
	}
	return ips, err
}

func main() {
	fmt.Println("Hello there stranger!")
	//lambda.Start(Handler)
	res, err := Handler(Request{
		ID: 123,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(res)
}
