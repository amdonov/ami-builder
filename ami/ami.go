package ami

import (
	"errors"
	"log"

	"github.com/amdonov/ami-builder/instance"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

func CreateAMI(config *instance.Config, provisioner instance.Provisioner) error {
	if "" == config.Subnet {
		return errors.New("subnet is required")
	}
	sess, err := session.NewSession()
	if err != nil {
		return err
	}
	ec2Service := ec2.New(sess)

	i, err := instance.Start(ec2Service, config)
	if err != nil {
		return err
	}

	// Create storage in the same AZ as the VM
	volumeParams := &ec2.CreateVolumeInput{
		AvailabilityZone: i.Instance.Placement.AvailabilityZone,
		VolumeType:       aws.String("gp2"),
		Size:             aws.Int64(20),
	}
	volResult, err := ec2Service.CreateVolume(volumeParams)
	if err != nil {
		return err
	}
	// Wait until volume is available
	err = ec2Service.WaitUntilVolumeAvailable(&ec2.DescribeVolumesInput{
		VolumeIds: []*string{volResult.VolumeId},
	})
	if err != nil {
		return err
	}
	// Attach storage
	attachParams := &ec2.AttachVolumeInput{
		Device:     aws.String("/dev/sdf"),
		VolumeId:   volResult.VolumeId,
		InstanceId: i.Instance.InstanceId,
	}
	_, err = ec2Service.AttachVolume(attachParams)
	if err != nil {
		return err
	}

	err = provisioner.Provision(i.IPAddress, i.Key)
	if err != nil {
		return err
	}
	err = instance.CleanUp(ec2Service, i)
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
