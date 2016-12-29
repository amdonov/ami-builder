package ami

import (
	"fmt"
	"log"
	"time"

	"golang.org/x/crypto/ssh"
)

func runCommand(client *ssh.Client, operation func(*ssh.Session) error) error {
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	return operation(session)
}

func connect(user, ip string, key []byte) (client *ssh.Client, err error) {
	// Create the Signer for this private key.
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		return
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
	for i := 0; i < 30; i = i + 1 {
		client, err = ssh.Dial("tcp", fmt.Sprintf("%s:22", ip), config)
		if err == nil {
			break
		}
		log.Println("SSH not available. Waiting...")
		time.Sleep(10 * time.Second)
	}
	return
}
