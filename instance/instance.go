package instance

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go/aws"
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

// Provisioner uses an SSH session to configure an AMI bootstrap instance.
type Provisioner interface {
	Provision(ip string, key []byte) error
}

type Server struct {
	Key           []byte
	IPAddress     string
	Instance      ec2.Instance
	keyName       string
	securityGroup *string
}

type Config struct {
	Subnet   string
	Name     string
	ImageID  string
	Size     string
	UserData string
	IAMRole  string
	Private  bool
}

func CleanUp(ec2Service *ec2.EC2, instance *Server) error {
	// Terminate the machine
	_, err := ec2Service.TerminateInstances(&ec2.TerminateInstancesInput{
		InstanceIds: []*string{instance.Instance.InstanceId},
	})
	if err != nil {
		return err
	}
	log.Println("Waiting for instance to terminate")
	err = ec2Service.WaitUntilInstanceTerminated(&ec2.DescribeInstancesInput{
		InstanceIds: []*string{instance.Instance.InstanceId},
	})
	if err != nil {
		return err
	}
	// Remove security group
	_, err = ec2Service.DeleteSecurityGroup(&ec2.DeleteSecurityGroupInput{
		GroupId: instance.securityGroup,
	})
	if err != nil {
		return err
	}
	// Remove key
	_, err = ec2Service.DeleteKeyPair(&ec2.DeleteKeyPairInput{KeyName: aws.String(instance.keyName)})
	if err != nil {
		return err
	}

	return nil
}

func Start(ec2Service *ec2.EC2, config *Config) (*Server, error) {
	// Create a key named bootstrap-Somenumber
	keyName, err := randomName()
	if err != nil {
		return nil, err
	}
	resp, err := ec2Service.CreateKeyPair(&ec2.CreateKeyPairInput{KeyName: aws.String(keyName)})
	if err != nil {
		return nil, err
	}
	// Spit out the private key for debugging failures
	fmt.Println(*resp.KeyMaterial)
	// Lookup the VPC so both subnet and vpc aren't required as parameters
	subnetReq := &ec2.DescribeSubnetsInput{
		SubnetIds: []*string{aws.String(config.Subnet)},
	}
	subnetResp, err := ec2Service.DescribeSubnets(subnetReq)
	if err != nil {
		return nil, err
	}
	vpc := subnetResp.Subnets[0].VpcId
	sgName, err := randomName()
	if err != nil {
		return nil, err
	}
	// Create a security group with public SSH access
	sgInput := &ec2.CreateSecurityGroupInput{
		VpcId:       vpc,
		Description: aws.String("Temporary SG for creating AMI"),
		GroupName:   aws.String(sgName),
	}
	sg, err := ec2Service.CreateSecurityGroup(sgInput)
	if err != nil {
		return nil, err
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
		return nil, err
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
	if config.IAMRole != "" {
		instanceParams.SetIamInstanceProfile(&ec2.IamInstanceProfileSpecification{
			Name: aws.String(config.IAMRole),
		})
	}
	if config.UserData != "" {
		instanceParams.SetUserData(config.UserData)
	}
	result, err := ec2Service.RunInstances(instanceParams)
	if err != nil {
		return nil, err
	}
	instance := *result.Instances[0]

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
				return nil, err
			}
			iface := interfaceDetails.NetworkInterfaces[0]
			if iface.Association != nil && iface.Association.PublicIp != nil {
				ipAddress = *iface.Association.PublicIp
				break
			}
			time.Sleep(10 * time.Second)
		}
	}

	log.Println("Waiting for instance to start")
	if err = ec2Service.WaitUntilInstanceRunning(&ec2.DescribeInstancesInput{
		InstanceIds: []*string{result.Instances[0].InstanceId},
	}); err != nil {
		return nil, err
	}

	// Everything is good return data to caller
	ai := &Server{
		Instance:      instance,
		IPAddress:     ipAddress,
		securityGroup: sg.GroupId,
		keyName:       keyName,
		Key:           []byte(*resp.KeyMaterial),
	}
	return ai, nil
}
