package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/amdonov/ami-builder/ami"

	"io/ioutil"

	"encoding/base64"

	cli "gopkg.in/urfave/cli.v1"
)

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
	}
	app.Commands = []cli.Command{
		{
			Name:  "cloud-init",
			Usage: "create a cloud-init based AMI",
			Action: func(c *cli.Context) error {
				config := &ami.Config{
					Subnet:      c.GlobalString("subnet"),
					Name:        c.GlobalString("name"),
					ImageID:     c.GlobalString("ami"),
					Size:        c.GlobalString("size"),
					Private:     c.GlobalBool("private"),
					Provisioner: ami.NewCloudInitProvisioner(c.GlobalString("user")),
				}
				return ami.CreateAMI(config)
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
				data, err := ioutil.ReadFile("cloud-data.yml")
				if err != nil {
					return err
				}
				// Confirm that the file is there to save some time
				if _, err := os.Stat(rpm); os.IsNotExist(err) {
					return fmt.Errorf("file path %s does not exist", rpm)
				}
				config := &ami.Config{
					Subnet:      c.GlobalString("subnet"),
					Name:        c.GlobalString("name"),
					ImageID:     c.GlobalString("ami"),
					Size:        c.GlobalString("size"),
					Private:     c.GlobalBool("private"),
					UserData:    base64.StdEncoding.EncodeToString(data),
					Provisioner: ami.NewProvClientProvisioner(c.GlobalString("user"), rpm, server),
				}
				return ami.CreateAMI(config)
			},
		},
	}
	app.Run(os.Args)
}
