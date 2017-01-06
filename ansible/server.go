package ansible

import (
	"errors"
	"fmt"
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
	user         string
	clientRPM    string
	serverRPM    string
	ami          string
	dns          string
	organization string
	realm        string
	domain       string
	password     string
	role         string
}

func NewAnsibleProvisioner(user, clientRPM, serverRPM, ami, dns, organization, realm, domain, password, role string) instance.Provisioner {
	return &ansible{user, clientRPM, serverRPM, ami, dns, organization, realm, domain, password, role}
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
		return session.Run(fmt.Sprintf("/bin/bash ./server.sh %s %s %s %s %s %s %s %s",
			c.password, c.domain, c.realm, c.organization, c.dns, c.ami, c.user, c.role))
	})
}

func makeRole(sess *session.Session, role string) error {
	svc := iam.New(sess)
	awsRole := aws.String(role)
	_, err := svc.CreateInstanceProfile(&iam.CreateInstanceProfileInput{
		InstanceProfileName: awsRole,
	})
	if err != nil {
		if err.(awserr.Error).Code() == "EntityAlreadyExists" {
			return nil
		}
		return err
	}

	params := &iam.CreateRoleInput{
		AssumeRolePolicyDocument: aws.String("{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Principal\":{\"Service\":\"ec2.amazonaws.com\"},\"Action\":\"sts:AssumeRole\"}]}"), // Required
		RoleName:                 awsRole,
	}
	_, err = svc.CreateRole(params)
	if err != nil {
		if err.(awserr.Error).Code() == "EntityAlreadyExists" {
			return nil
		}
		return err
	}
	_, err = svc.AddRoleToInstanceProfile(&iam.AddRoleToInstanceProfileInput{
		InstanceProfileName: awsRole,
		RoleName:            awsRole,
	})
	if err != nil {
		return err
	}
	_, err = svc.PutRolePolicy(&iam.PutRolePolicyInput{
		PolicyDocument: aws.String("{\"Version\":\"2012-10-17\",\"Statement\":[{\"Effect\":\"Allow\",\"Action\":[\"ec2:*\"],\"Resource\":[\"*\"]},{\"Effect\":\"Allow\",\"Action\":[\"iam:PassRole\"],\"Resource\":[\"*\"]}]}"),
		PolicyName:     aws.String("anything-in-ec2"),
		RoleName:       awsRole,
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
	err = makeRole(sess, config.IAMRole)
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
