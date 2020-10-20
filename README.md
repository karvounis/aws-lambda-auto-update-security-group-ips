# aws-lambda-auto-update-security-group-ips

This repo contains a Golang Lambda function that automatically updates (adds/removes) Security Group's rules with the 
public IPs of the instances of an AWS Autoscaling Group.

Whenever a new EC2 instance, with a public IP, is created, a new security group rule will be added to the SG.
Whenever an EC2 instance, with a public IP, is terminated, the security group rule for that IP will be removed from 
the SG.

It is listening for CloudWatch events (EventBridge) that trigger when an instance passes through either the launching 
or terminating states.

This function is particularly helpful when you have a cluster of EC2 instances and you want to automatically allow 
access to and from them by updating the Security Group's rules.

The blog https://aws.amazon.com/blogs/compute/automating-security-group-updates-with-aws-lambda/ was the inspiration
for this Golang Lambda function.

## Lambda Environmental Variables
* securityGroupID: The ID of the Security Group

## Example CloudWatch Event
```json
    {
        "account": "12345678912",
        "region": "us-east-1",
        "version": "0",
        "id": "3122afb6-be7l-47e8-1cb8-a5c437bd109d",
        "detail-type": "EC2 Instance-launch Lifecycle Action",
        "source": "aws.autoscaling",
        "resources": [
            "arn:aws:autoscaling:us-east-1:12345678912:autoScalingGroup:d3fe9d10-34d0-4c62-b9bb-293b41ba3781:autoScalingGroupName/test-lambda-asg"
        ],
        "detail": {
            "LifecycleHookName": "lifecycle-hook-launch",
            "AutoScalingGroupName": "test-lambda-asg",
            "LifecycleActionToken": "33965228-086a-4aeb-8c26-f82ed3bef491",
            "LifecycleTransition": "autoscaling:EC2_INSTANCE_LAUNCHING",
            "EC2InstanceId": "i-00bd018f38bvcf1c5"
        },
        "time": "2020-10-20T05:47:36Z"
    }
```
