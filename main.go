package main

import (
	"errors"
	"fmt"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/autoscaling"
	"github.com/aws/aws-sdk-go/service/ec2"
	"go.uber.org/zap"
	"os"
)

type Request struct {
	ID float64 `json:"id"`
	Value string `json:"value"`
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

	autoscalingSvc := autoscaling.New(sess, &aws.Config{Region: aws.String(region)})
	ec2Svc := ec2.New(sess, &aws.Config{Region: aws.String(region)})

	asgResponse, err := autoscalingSvc.DescribeAutoScalingGroups(&autoscaling.DescribeAutoScalingGroupsInput{
		AutoScalingGroupNames: []*string{aws.String(asgName)},
	})
	logger.Info("asd", zap.Any("asd", asgResponse))
	if err != nil {
		logger.Error("Failed to describe autoscaling groups", zap.Error(err))
		return response, err
	}
	if asgResponse.String() == "{\n\n}" {
		err = errors.New("autoscaling group response is empty")
		logger.Error("Error", zap.Error(err))
		return response, err
	}

	sgResponse, err := ec2Svc.DescribeSecurityGroups(&ec2.DescribeSecurityGroupsInput{
		GroupIds:   []*string{
			aws.String(sgID),
		},
	})
	if err != nil {
		logger.Error("Failed to describe security groups", zap.Error(err))
		return response, err
	}
	logger.Info("asd", zap.Any("asd", sgResponse))

	// Check number of instances. If 0, move to SG update
	numberOfAsgInstances := len(asgResponse.AutoScalingGroups[0].Instances)
	if numberOfAsgInstances == 0 {
		logger.Info("No instances are running")

		if len(sgResponse.SecurityGroups[0].IpPermissions) != 0 {
			_, err := ec2Svc.RevokeSecurityGroupIngress(&ec2.RevokeSecurityGroupIngressInput{
				GroupId: aws.String(sgID),
				IpPermissions: sgResponse.SecurityGroups[0].IpPermissions,
			})
			if err != nil {
				logger.Error("Failed to revoke all security group Ingress", zap.Error(err))
				return response, err
			}
			logger.Info(fmt.Sprintf("Deleted all security group rules from SG %s because there were no instances running for the ASG %s", sgID, asgName))
		}
		return response, err
	} else {
		// If more than 0, fetch the instances from EC2
		// Loop through the asgResponse.AutoScalingGroups[0].Instances[i]
		//ec2Response, err := ec2Svc.DescribeInstances(&ec2.DescribeInstancesInput{
		//	InstanceIds: []*string{
		//		asgResponse.AutoScalingGroups[0].Instances[0].InstanceId,
		//		asgResponse.AutoScalingGroups[0].Instances[1].InstanceId,
		//	},
		//})
		//if err != nil {
		//	logger.Error("Failed to describe EC2 instances", zap.Error(err))
		//	return response, err
		//}
		//
		//allPublicIPs := make([]*string, len(asgResponse.AutoScalingGroups[0].Instances))
		////fmt.Println(ec2Response.Reservations)
		//
		//for i, reservation := range ec2Response.Reservations {
		//	//fmt.Println(reservation)
		//	allPublicIPs[i] = reservation.Instances[0].PublicIpAddress
		//}
		//fmt.Println(*allPublicIPs[0], *allPublicIPs[1])
		//fmt.Println(len(ec2Response.Reservations))
		//fmt.Println(allPublicIPs)
	}

	sg := sgResponse.SecurityGroups[0]
	allSGCidrs := make([]*string, len(sg.IpPermissions))
	fmt.Println(allSGCidrs)
	for i, ipPermission := range sg.IpPermissions {
		//fmt.Println(reservation)
		allSGCidrs[i] = ipPermission.IpRanges[0].CidrIp
	}
	fmt.Println(sg.IpPermissions[0])

	//input := &ec2.AuthorizeSecurityGroupIngressInput{
	//	GroupId: aws.String(sgID),
	//	FromPort: aws.Int64(0),
	//	ToPort: aws.Int64(65535),
	//	CidrIp: aws.String(fmt.Sprintf("%s/32", *allPublicIPs[0])),
	//	IpProtocol: aws.String("tcp"),
	//}
	//fmt.Println(res)
	//
	//res2, err := ec2Svc.AuthorizeSecurityGroupIngress(input)
	//if err != nil {
	//	fmt.Println("Failed to update security group")
	//	return response, err
	//}
	//fmt.Println(res2)

	return "All ok!", err
}

func main() {
	fmt.Print("Hello there stranger!")
	//lambda.Start(Handler)
	res, err := Handler(Request{
		ID: 123,
	})
	if err != nil {
		fmt.Println(err.Error())
		os.Exit(1)
	}
	fmt.Sprint(res)
}
