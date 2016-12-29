package ami

import (
	"os"

	"github.com/tmc/scp"
	"golang.org/x/crypto/ssh"
)

type provClient struct {
	user string
	rpm  string
}

func NewProvClientProvisioner(user, rpm string) Provisioner {
	return &provClient{user, rpm}
}

func (c *provClient) Provision(ip string, key []byte) error {
	client, err := connect(c.user, ip, key)
	if err != nil {
		return err
	}
	defer client.Close()
	err = runCommand(client, func(session *ssh.Session) error {
		return scp.CopyPath(c.rpm, "/tmp/prov-client.rpm", session)
	})
	if err != nil {
		return err
	}
	err = runCommand(client, func(session *ssh.Session) error {
		return scp.CopyPath("ami-iaas.sh", "~/ami.sh", session)
	})
	if err != nil {
		return err
	}
	return runCommand(client, func(session *ssh.Session) error {
		session.Stdout = os.Stdout
		return session.Run("sudo /bin/bash ./ami.sh")
	})
}
