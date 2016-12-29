package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

func randomName() (res string, err error) {
	b := make([]byte, 4)
	_, err = rand.Read(b)
	if err != nil {
		return
	}
	res = fmt.Sprintf("bootstrap-%s", hex.EncodeToString(b))
	return
}

func main() {
	amiName := "CentOS 7.3"
	subnet := "subnet-d5694593"
	size := "t2.micro"
	imageID := "ami-9be6f38c"
	sess, err := session.NewSession()
	if err != nil {
		log.Fatal(err)
	}
	ec2Service := ec2.New(sess)
	// Create a key named bootstrap-Somenumber
	keyName, err := randomName()
	if err != nil {
		log.Fatal(err)
	}
	resp, err := ec2Service.CreateKeyPair(&ec2.CreateKeyPairInput{KeyName: aws.String(keyName)})
	if err != nil {
		log.Fatal(err)
	}
	// Spit out the private key for debugging failures
	fmt.Println(*resp.KeyMaterial)
	// Lookup the VPC so both subnet and vpc aren't required as parameters
	subnetReq := &ec2.DescribeSubnetsInput{
		SubnetIds: []*string{aws.String(subnet)},
	}
	subnetResp, err := ec2Service.DescribeSubnets(subnetReq)
	if err != nil {
		log.Fatal(err)
	}
	vpc := subnetResp.Subnets[0].VpcId
	sgName, err := randomName()
	if err != nil {
		log.Fatal(err)
	}
	// Create a security group with public SSH access
	sgInput := &ec2.CreateSecurityGroupInput{
		VpcId:       vpc,
		Description: aws.String("Temporary SG for creating AMI"),
		GroupName:   aws.String(sgName),
	}
	sg, err := ec2Service.CreateSecurityGroup(sgInput)
	if err != nil {
		log.Fatal(err)
	}
	authInput := &ec2.AuthorizeSecurityGroupIngressInput{
		CidrIp:     aws.String("0.0.0.0/0"),
		GroupId:    sg.GroupId,
		IpProtocol: aws.String("tcp"),
		FromPort:   aws.Int64(22),
		ToPort:     aws.Int64(22),
	}
	_, err = ec2Service.AuthorizeSecurityGroupIngress(authInput)
	if err != nil {
		log.Fatal(err)
	}
	// This is the private key
	fmt.Println(*resp.KeyMaterial)
	// Provision a machine named bootstrap-Somenumber
	instanceParams := &ec2.RunInstancesInput{
		KeyName:          aws.String(keyName),
		ImageId:          aws.String(imageID),
		InstanceType:     aws.String(size),
		MaxCount:         aws.Int64(1),
		MinCount:         aws.Int64(1),
		SubnetId:         aws.String(subnet),
		SecurityGroupIds: []*string{sg.GroupId},
	}
	result, err := ec2Service.RunInstances(instanceParams)
	if err != nil {
		log.Fatal(err)
	}
	instance := *result.Instances[0]
	// Create storage in the same AZ as the VM
	volumeParams := &ec2.CreateVolumeInput{
		AvailabilityZone: instance.Placement.AvailabilityZone,
		VolumeType:       aws.String("gp2"),
		Size:             aws.Int64(20),
	}
	volResult, err := ec2Service.CreateVolume(volumeParams)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Waiting for instance to start")
	// Wait until the instance is running
	err = ec2Service.WaitUntilInstanceRunning(&ec2.DescribeInstancesInput{
		InstanceIds: []*string{instance.InstanceId},
	})
	if err != nil {
		log.Fatal(err)
	}
	// Attach storage
	attachParams := &ec2.AttachVolumeInput{
		Device:     aws.String("/dev/sdf"),
		VolumeId:   volResult.VolumeId,
		InstanceId: instance.InstanceId,
	}
	_, err = ec2Service.AttachVolume(attachParams)
	if err != nil {
		log.Fatal(err)
	}

	// Copy shell script to VM and install OS
	err = install("ec2-user", *instance.PrivateIpAddress, []byte(*resp.KeyMaterial))
	if err != nil {
		log.Fatal(err)
	}
	// Terminate the machine
	_, err = ec2Service.TerminateInstances(&ec2.TerminateInstancesInput{
		InstanceIds: []*string{instance.InstanceId},
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Waiting for instance to terminate")
	err = ec2Service.WaitUntilInstanceTerminated(&ec2.DescribeInstancesInput{
		InstanceIds: []*string{instance.InstanceId},
	})
	if err != nil {
		log.Fatal(err)
	}
	// Remove security group
	_, err = ec2Service.DeleteSecurityGroup(&ec2.DeleteSecurityGroupInput{
		GroupId: sg.GroupId,
	})
	if err != nil {
		log.Fatal(err)
	}
	// Remove key
	_, err = ec2Service.DeleteKeyPair(&ec2.DeleteKeyPairInput{KeyName: aws.String(keyName)})
	if err != nil {
		log.Fatal(err)
	}
	// snapshot volume
	snapshot, err := ec2Service.CreateSnapshot(&ec2.CreateSnapshotInput{
		VolumeId:    volResult.VolumeId,
		Description: aws.String(amiName),
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Println("Waiting for snapshot to complete")
	err = ec2Service.WaitUntilSnapshotCompleted(&ec2.DescribeSnapshotsInput{
		SnapshotIds: []*string{snapshot.SnapshotId},
	})
	if err != nil {
		log.Fatal(err)
	}
	// delete the volume
	_, err = ec2Service.DeleteVolume(&ec2.DeleteVolumeInput{
		VolumeId: volResult.VolumeId,
	})
	if err != nil {
		log.Fatal(err)
	}
}
