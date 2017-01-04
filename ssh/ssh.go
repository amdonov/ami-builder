package ssh

import (
	"fmt"
	"log"
	"time"

	"golang.org/x/crypto/ssh"
)

type Client struct {
	c *ssh.Client
}

func (c *Client) RunCommand(operation func(*ssh.Session) error) error {
	session, err := c.c.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	return operation(session)
}

func (c *Client) Close() {
	c.Close()
}

func Connect(user, ip string, key []byte) (*Client, error) {
	// Create the Signer for this private key.
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return nil, err
	}

	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			// Use the PublicKeys method for remote authentication.
			ssh.PublicKeys(signer),
		},
	}

	// Connect to the remote server and perform the SSH handshake.
	// Try every 10 seconds up to five minutes
	var client *ssh.Client
	for i := 0; i < 30; i = i + 1 {
		client, err = ssh.Dial("tcp", fmt.Sprintf("%s:22", ip), config)
		if err == nil {
			return &Client{client}, nil
		}
		log.Println("SSH not available. Waiting...")
		time.Sleep(10 * time.Second)
	}
	return nil, err
}
