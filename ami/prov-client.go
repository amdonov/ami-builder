package ami

import (
	"fmt"
	"os"

	"golang.org/x/crypto/ssh"

	"github.com/amdonov/ami-builder/instance"
	myssh "github.com/amdonov/ami-builder/ssh"

	"github.com/tmc/scp"
)

type provClient struct {
	user   string
	rpm    string
	server string
	repo   string
}

func NewProvClientProvisioner(user, rpm, server, repo string) instance.Provisioner {
	return &provClient{user, rpm, server, repo}
}

func (c *provClient) Provision(ip string, key []byte) error {
	client, err := myssh.Connect(c.user, ip, key)
	if err != nil {
		return err
	}
	defer client.Close()
	err = client.RunCommand(func(session *ssh.Session) error {
		return scp.CopyPath(c.rpm, "/tmp/prov-client.rpm", session)
	})
	if err != nil {
		return err
	}
	err = client.RunCommand(func(session *ssh.Session) error {
		return scp.CopyPath("ami-iaas.sh", "~/ami.sh", session)
	})
	if err != nil {
		return err
	}
	return client.RunCommand(func(session *ssh.Session) error {
		session.Stdout = os.Stdout
		return session.Run(fmt.Sprintf("sudo /bin/bash ./ami.sh %s %s", c.server, c.repo))
	})
}
