package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/amdonov/ami-builder/ami"
	"github.com/amdonov/ami-builder/ansible"

	"encoding/base64"

	"github.com/amdonov/ami-builder/instance"
	cli "gopkg.in/urfave/cli.v1"
)

const cloudData = `#cloud-config
manage_etc_hosts: true
manage_resolv_conf: true
resolv_conf:
  nameservers:
   - %s
  options:
    rotate: true
    timeout: 1`

func main() {
	app := cli.NewApp()
	app.Name = "ami-builder"
	app.Version = "0.2.0"
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
		cli.StringFlag{
			Name:   "user, u",
			Value:  "ec2-user",
			Usage:  "privileged user on bootstrap AMI",
			EnvVar: "AMI_USER"},
		cli.BoolFlag{
			Name:   "private",
			Usage:  "connect to bootstrap machine via private IP",
			EnvVar: "AMI_PRIVATE",
		},
		cli.StringFlag{
			Name:   "repo, r",
			Value:  "default",
			Usage:  "Local IP address of server containing software",
			EnvVar: "REPO_SERVER",
		},
	}
	app.Commands = []cli.Command{
		{
			Name:  "cloud-init",
			Usage: "create a cloud-init based AMI",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "newuser",
					Value: "ec2-user",
					Usage: "privileged user in newly created AMI",
				},
			},
			Action: func(c *cli.Context) error {
				config := &instance.Config{
					Subnet:  c.GlobalString("subnet"),
					Name:    c.GlobalString("name"),
					ImageID: c.GlobalString("ami"),
					Size:    c.GlobalString("size"),
					Private: c.GlobalBool("private"),
				}
				return ami.CreateAMI(config, ami.NewCloudInitProvisioner(c.GlobalString("user"), c.String("newuser"), c.GlobalString("repo")))
			},
		},
		{
			Name:  "prov-server",
			Usage: "create a prov-server instance and core infrastructure",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "server-rpm",
					Value: "",
					Usage: "path to provision-server RPM",
				},
				cli.StringFlag{
					Name:  "client-rpm",
					Value: "",
					Usage: "path to client-server RPM",
				},
				cli.StringFlag{
					Name:  "iam",
					Value: "ansible",
					Usage: "IAM role name for provision-server",
				},
				cli.StringFlag{
					Name:  "password",
					Value: "changeme",
					Usage: "adminstrator password for IPA and Foreman",
				},
				cli.StringFlag{
					Name:  "domain",
					Value: "example.com",
					Usage: "domain name for servers",
				},
				cli.StringFlag{
					Name:  "dns",
					Value: "8.8.8.8",
					Usage: "DNS server for external requests",
				},
				cli.StringFlag{
					Name:  "realm",
					Value: "EXAMPLE.COM",
					Usage: "Kerberos realm for servers",
				},
				cli.StringFlag{
					Name:  "org",
					Value: "MyOrg",
					Usage: "initial organization in Foreman",
				},
			},
			Action: func(c *cli.Context) error {
				serverRPM := c.String("server-rpm")
				if "" == serverRPM {
					return errors.New("server-rpm argument is required")
				}
				clientRPM := c.String("client-rpm")
				if "" == clientRPM {
					return errors.New("client-rpm argument is required")
				}
				data := []byte(fmt.Sprintf(cloudData, c.String("dns")))
				// Confirm that the file is there to save some time
				if _, err := os.Stat(serverRPM); os.IsNotExist(err) {
					return fmt.Errorf("file path %s does not exist", serverRPM)
				}
				if _, err := os.Stat(clientRPM); os.IsNotExist(err) {
					return fmt.Errorf("file path %s does not exist", clientRPM)
				}
				config := &instance.Config{
					Subnet:   c.GlobalString("subnet"),
					Name:     c.GlobalString("name"),
					ImageID:  c.GlobalString("ami"),
					Size:     c.GlobalString("size"),
					Private:  c.GlobalBool("private"),
					IAMRole:  c.String("iam"),
					UserData: base64.StdEncoding.EncodeToString(data),
				}
				return ansible.CreateProvisionServer(config,
					ansible.NewAnsibleProvisioner(c.GlobalString("user"), clientRPM, serverRPM,
						c.GlobalString("ami"), c.String("dns"), c.String("org"), c.String("realm"),
						c.String("domain"), c.String("password"), c.String("iam"), c.GlobalString("repo")))
			},
		},
		{
			Name:  "prov-client",
			Usage: "create a prov-client based AMI",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "rpm",
					Value: "",
					Usage: "path to provision-client RPM",
				},
				cli.StringFlag{
					Name:  "server",
					Value: "",
					Usage: "IP address of provision server",
				},
				cli.StringFlag{
					Name:  "dns",
					Value: "8.8.8.8",
					Usage: "DNS server for external requests",
				},
			},
			Action: func(c *cli.Context) error {
				rpm := c.String("rpm")
				server := c.String("server")
				if "" == rpm {
					return errors.New("rpm argument is required")
				}
				if "" == server {
					return errors.New("server argument is required")
				}
				data := []byte(fmt.Sprintf(cloudData, c.String("dns")))
				// Confirm that the file is there to save some time
				if _, err := os.Stat(rpm); os.IsNotExist(err) {
					return fmt.Errorf("file path %s does not exist", rpm)
				}
				config := &instance.Config{
					Subnet:   c.GlobalString("subnet"),
					Name:     c.GlobalString("name"),
					ImageID:  c.GlobalString("ami"),
					Size:     c.GlobalString("size"),
					Private:  c.GlobalBool("private"),
					UserData: base64.StdEncoding.EncodeToString(data),
				}
				return ami.CreateAMI(config, ami.NewProvClientProvisioner(c.GlobalString("user"), rpm, server, c.GlobalString("repo")))
			},
		},
	}
	app.Run(os.Args)
}
