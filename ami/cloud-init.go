package ami

import (
	"os"

	"github.com/tmc/scp"
	"golang.org/x/crypto/ssh"
)

type cloudInit struct {
	user string
}

func NewCloudInitProvisioner(user string) Provisioner {
	return &cloudInit{user}
}

func (c *cloudInit) Provision(ip string, key []byte) error {
	client, err := connect(c.user, ip, key)
	if err != nil {
		return err
	}
	defer client.Close()
	err = runCommand(client, func(session *ssh.Session) error {
		return scp.CopyPath("ami.sh", "~/ami.sh", session)
	})
	if err != nil {
		return err
	}
	return runCommand(client, func(session *ssh.Session) error {
		session.Stdout = os.Stdout
		return session.Run("sudo /bin/bash ./ami.sh")
	})
}
