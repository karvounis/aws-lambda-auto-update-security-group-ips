# aws-lambda-auto-whitelist-ips

Lambda function, written in Golang, that automatically syncs Security Group's rules with the public IPs of the instances of an AWS Autoscaling Group.

Whenever a new EC2 instance, with a public IP, is created, a new security group rule will be added to the SG.
Whenever an EC2 instance, with a public IP, is terminated, the security group rule for that IP will be removed from the SG.

This blog https://aws.amazon.com/blogs/compute/automating-security-group-updates-with-aws-lambda/ was the inspiration
for this Golang Lambda function.

## Lambda Environmental Variables
* securityGroupID: The ID of the Security Group
