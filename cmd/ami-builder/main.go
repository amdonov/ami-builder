package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"

	cli "gopkg.in/urfave/cli.v1"

	"time"

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
	app := cli.NewApp()
	app.Name = "ami-builder"
	app.Version = "0.1.0"
	app.Usage = "create RHEL/CentOS-based AMI"
	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:   "subnet",
			Value:  "",
			Usage:  "bootstrap machine subnet id",
			EnvVar: "AMI_SUBNET"},
		cli.StringFlag{
			Name:   "name, n",
			Value:  "CentOS 7.3",
			Usage:  "ami and snapshot name",
			EnvVar: "AMI_NAME"},
		cli.StringFlag{
			Name:   "size, s",
			Value:  "t2.micro",
			Usage:  "bootstrap machine size",
			EnvVar: "AMI_SIZE"},
		cli.StringFlag{
			Name:   "ami, a",
			Value:  "ami-9be6f38c",
			Usage:  "bootstrap machine AMI",
			EnvVar: "AMI_IMAGE"},
		cli.BoolFlag{
			Name:   "private",
			Usage:  "connect to bootstrap machine via private IP",
			EnvVar: "AMI_PRIVATE",
		},
	}
	app.Action = func(c *cli.Context) error {
		subnet := c.GlobalString("subnet")
		if "" == subnet {
			return errors.New("subnet is required")
		}
		amiName := c.GlobalString("name")
		imageID := c.GlobalString("ami")
		size := c.GlobalString("size")
		private := c.GlobalBool("private")
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
			SubnetIds: []*string{aws.String(subnet)},
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
			ImageId:      aws.String(imageID),
			InstanceType: aws.String(size),
			MaxCount:     aws.Int64(1),
			MinCount:     aws.Int64(1),
			NetworkInterfaces: []*ec2.InstanceNetworkInterfaceSpecification{
				&ec2.InstanceNetworkInterfaceSpecification{
					AssociatePublicIpAddress: aws.Bool(!private),
					DeviceIndex:              aws.Int64(0),
					SubnetId:                 aws.String(subnet),
					Groups:                   []*string{sg.GroupId},
				},
			},
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
		if private {
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

		// Copy shell script to VM and install OS
		err = install("ec2-user", ipAddress, []byte(*resp.KeyMaterial))
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
			Description: aws.String(amiName),
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
			Name:               aws.String(amiName),
			Description:        aws.String(amiName),
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
	app.Run(os.Args)

}
