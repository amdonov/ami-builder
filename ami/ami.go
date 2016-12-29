package ami

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// Provisioner uses an SSH session to configure an AMI bootstrap instance.
type Provisioner interface {
	Provision(ip string, key []byte) error
}

type Config struct {
	Provisioner Provisioner
	Subnet      string
	Name        string
	ImageID     string
	Size        string
	UserData    string
	Private     bool
}

func randomName() (res string, err error) {
	b := make([]byte, 4)
	_, err = rand.Read(b)
	if err != nil {
		return
	}
	res = fmt.Sprintf("bootstrap-%s", hex.EncodeToString(b))
	return
}

func CreateAMI(config *Config) error {
	if "" == config.Subnet {
		return errors.New("subnet is required")
	}
	sess, err := session.NewSession()
	if err != nil {
		return err
	}
	ec2Service := ec2.New(sess)
	// Create a key named bootstrap-Somenumber
	keyName, err := randomName()
	if err != nil {
		return err
	}
	resp, err := ec2Service.CreateKeyPair(&ec2.CreateKeyPairInput{KeyName: aws.String(keyName)})
	if err != nil {
		return err
	}
	// Spit out the private key for debugging failures
	fmt.Println(*resp.KeyMaterial)
	// Lookup the VPC so both subnet and vpc aren't required as parameters
	subnetReq := &ec2.DescribeSubnetsInput{
		SubnetIds: []*string{aws.String(config.Subnet)},
	}
	subnetResp, err := ec2Service.DescribeSubnets(subnetReq)
	if err != nil {
		return err
	}
	vpc := subnetResp.Subnets[0].VpcId
	sgName, err := randomName()
	if err != nil {
		return err
	}
	// Create a security group with public SSH access
	sgInput := &ec2.CreateSecurityGroupInput{
		VpcId:       vpc,
		Description: aws.String("Temporary SG for creating AMI"),
		GroupName:   aws.String(sgName),
	}
	sg, err := ec2Service.CreateSecurityGroup(sgInput)
	if err != nil {
		return err
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
		return err
	}
	// Provision a machine named bootstrap-Somenumber
	instanceParams := &ec2.RunInstancesInput{
		KeyName:      aws.String(keyName),
		ImageId:      aws.String(config.ImageID),
		InstanceType: aws.String(config.Size),
		MaxCount:     aws.Int64(1),
		MinCount:     aws.Int64(1),
		NetworkInterfaces: []*ec2.InstanceNetworkInterfaceSpecification{
			&ec2.InstanceNetworkInterfaceSpecification{
				AssociatePublicIpAddress: aws.Bool(!config.Private),
				DeviceIndex:              aws.Int64(0),
				SubnetId:                 aws.String(config.Subnet),
				Groups:                   []*string{sg.GroupId},
			},
		},
	}
	if config.UserData != "" {
		instanceParams.SetUserData(config.UserData)
	}
	result, err := ec2Service.RunInstances(instanceParams)
	if err != nil {
		return err
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
		return err
	}
	log.Println("Waiting for instance to start")
	// Wait until the instance is running
	err = ec2Service.WaitUntilInstanceRunning(&ec2.DescribeInstancesInput{
		InstanceIds: []*string{instance.InstanceId},
	})
	if err != nil {
		return err
	}
	// Attach storage
	attachParams := &ec2.AttachVolumeInput{
		Device:     aws.String("/dev/sdf"),
		VolumeId:   volResult.VolumeId,
		InstanceId: instance.InstanceId,
	}
	_, err = ec2Service.AttachVolume(attachParams)
	if err != nil {
		return err
	}
	var ipAddress string
	if config.Private {
		ipAddress = *instance.PrivateIpAddress
	} else {
		log.Println("Waiting for public IP")
		for i := 0; i < 10; i = i + 1 {
			interfaceDetails, err := ec2Service.DescribeNetworkInterfaces(&ec2.DescribeNetworkInterfacesInput{
				NetworkInterfaceIds: []*string{instance.NetworkInterfaces[0].NetworkInterfaceId},
			})
			if err != nil {
				return err
			}
			iface := interfaceDetails.NetworkInterfaces[0]
			if iface.Association != nil && iface.Association.PublicIp != nil {
				ipAddress = *iface.Association.PublicIp
				break
			}
			time.Sleep(10 * time.Second)
		}
	}

	err = config.Provisioner.Provision(ipAddress, []byte(*resp.KeyMaterial))
	if err != nil {
		return err
	}
	// Terminate the machine
	_, err = ec2Service.TerminateInstances(&ec2.TerminateInstancesInput{
		InstanceIds: []*string{instance.InstanceId},
	})
	if err != nil {
		return err
	}
	log.Println("Waiting for instance to terminate")
	err = ec2Service.WaitUntilInstanceTerminated(&ec2.DescribeInstancesInput{
		InstanceIds: []*string{instance.InstanceId},
	})
	if err != nil {
		return err
	}
	// Remove security group
	_, err = ec2Service.DeleteSecurityGroup(&ec2.DeleteSecurityGroupInput{
		GroupId: sg.GroupId,
	})
	if err != nil {
		return err
	}
	// Remove key
	_, err = ec2Service.DeleteKeyPair(&ec2.DeleteKeyPairInput{KeyName: aws.String(keyName)})
	if err != nil {
		return err
	}
	// snapshot volume
	snapshot, err := ec2Service.CreateSnapshot(&ec2.CreateSnapshotInput{
		VolumeId:    volResult.VolumeId,
		Description: aws.String(config.Name),
	})
	if err != nil {
		return err
	}
	log.Println("Waiting for snapshot to complete")
	err = ec2Service.WaitUntilSnapshotCompleted(&ec2.DescribeSnapshotsInput{
		SnapshotIds: []*string{snapshot.SnapshotId},
	})
	if err != nil {
		return err
	}
	// delete the volume
	_, err = ec2Service.DeleteVolume(&ec2.DeleteVolumeInput{
		VolumeId: volResult.VolumeId,
	})
	if err != nil {
		return err
	}

	// Register the AMI
	regResult, err := ec2Service.RegisterImage(&ec2.RegisterImageInput{
		Name:               aws.String(config.Name),
		Description:        aws.String(config.Name),
		Architecture:       aws.String("x86_64"),
		RootDeviceName:     aws.String("/dev/sda1"),
		VirtualizationType: aws.String("hvm"),
		BlockDeviceMappings: []*ec2.BlockDeviceMapping{
			{ // Required
				DeviceName: aws.String("/dev/sda1"),
				Ebs: &ec2.EbsBlockDevice{
					DeleteOnTermination: aws.Bool(true),
					SnapshotId:          snapshot.SnapshotId,
					VolumeSize:          aws.Int64(20),
					VolumeType:          aws.String("gp2"),
				},
			},
		},
	})
	if err != nil {
		return err
	}
	log.Printf("AMI registered with id of %s", *regResult.ImageId)
	return nil
}
