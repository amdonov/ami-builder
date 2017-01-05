package ansible

import (
	"errors"
	"os"

	"golang.org/x/crypto/ssh"

	"github.com/amdonov/ami-builder/instance"
	myssh "github.com/amdonov/ami-builder/ssh"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/aws/aws-sdk-go/service/iam"
	"github.com/tmc/scp"
)

type ansible struct {
	user      string
	clientRPM string
	serverRPM string
}

func NewAnsibleProvisioner(user, clientRPM, serverRPM string) instance.Provisioner {
	return &ansible{user, clientRPM, serverRPM}
}

func (c *ansible) Provision(ip string, key []byte) error {
	client, err := myssh.Connect(c.user, ip, key)
	if err != nil {
		return err
	}
	defer client.Close()
	files := make(map[string]string)
	files[c.serverRPM] = "/tmp/prov-server.rpm"
	files[c.clientRPM] = "/tmp/prov-client.rpm"

	files["server.sh"] = "~/server.sh"
	for src, dest := range files {
		err = client.RunCommand(func(session *ssh.Session) error {
			return scp.CopyPath(src, dest, session)
		})
		if err != nil {
			return err
		}
	}
	return client.RunCommand(func(session *ssh.Session) error {
		session.Stdout = os.Stdout
		return session.Run("sudo /bin/bash ./server.sh")
	})
}

func makeRole(sess *session.Session) error {
	svc := iam.New(sess)

	_, err := svc.CreateInstanceProfile(&iam.CreateInstanceProfileInput{
		InstanceProfileName: aws.String("ansible"),
	})
	if err != nil {
		if err.(awserr.Error).Code() == "EntityAlreadyExists" {
			return nil
		}
		return err
	}

	params := &iam.CreateRoleInput{
		AssumeRolePolicyDocument: aws.String("{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Principal\":{\"Service\":\"ec2.amazonaws.com\"},\"Action\":\"sts:AssumeRole\"}]}"), // Required
		RoleName:                 aws.String("ansible"),
	}
	_, err = svc.CreateRole(params)
	if err != nil {
		if err.(awserr.Error).Code() == "EntityAlreadyExists" {
			return nil
		}
		return err
	}
	_, err = svc.AddRoleToInstanceProfile(&iam.AddRoleToInstanceProfileInput{
		InstanceProfileName: aws.String("ansible"),
		RoleName:            aws.String("ansible"),
	})
	if err != nil {
		return err
	}
	_, err = svc.PutRolePolicy(&iam.PutRolePolicyInput{
		PolicyDocument: aws.String("{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Action\":[\"ec2:*\"],\"Resource\":[\"*\"]},{\"Effect\":\"Allow\",\"Action\":[\"iam:PassRole\"],\"Resource\":[\"*\"]}]}"),
		PolicyName:     aws.String("anything-in-ec2"),
		RoleName:       aws.String("ansible"),
	})
	return err
}

func CreateProvisionServer(config *instance.Config, provisioner instance.Provisioner) error {
	if "" == config.Subnet {
		return errors.New("subnet is required")
	}
	sess, err := session.NewSession()
	if err != nil {
		return err
	}
	err = makeRole(sess)
	if err != nil {
		return err
	}

	ec2Service := ec2.New(sess)

	i, err := instance.Start(ec2Service, config)
	if err != nil {
		return err
	}
	err = provisioner.Provision(i.IPAddress, i.Key)
	if err != nil {
		return err
	}
	return instance.CleanUp(ec2Service, i)
}
