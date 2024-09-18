package sshrunner

import (
	"context"
	"fmt"
	"github.com/virtual-kubelet/virtual-kubelet/node/api"
	"golang.org/x/crypto/ssh"
	v1 "k8s.io/api/core/v1"
	"log"
	"net"
	"strings"
	"time"
)

type Runner struct {
}

type AttachIO api.AttachIO

type DialInfo struct {
	Address  string
	Username string
	Password string
}

func NewRunner() *Runner {
	return &Runner{}
}

// handleResize listens for resize events from the resize channel and adjusts the terminal size accordingly.
func handleSSHWindowResize(session *ssh.Session, resize <-chan api.TermSize) {
	for termSize := range resize {
		// Send the window change request to the SSH session with the new terminal size
		if err := session.WindowChange(int(termSize.Height), int(termSize.Width)); err != nil {
			log.Printf("failed to change window size: %v\n", err)
		}
	}
	log.Printf("window resize terminated")
}

func attachStreams(session *ssh.Session, attach AttachIO) error {
	if attach == nil {
		return nil
	}
	// Set up the input/output streams
	session.Stdin = attach.Stdin()
	session.Stdout = attach.Stdout()
	session.Stderr = attach.Stderr()

	if attach.TTY() {
		modes := ssh.TerminalModes{
			ssh.ECHO:          1,     // enable echoing
			ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
			ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
		}

		if err := session.RequestPty("xterm-256color", 80, 40, modes); err != nil {
			return fmt.Errorf("request for pseudo terminal failed: %w", err)
		}
		go handleSSHWindowResize(session, attach.Resize())
	}
	return nil
}

func setEnvVars(session *ssh.Session, env []v1.EnvVar, isSensitive func(name string) bool) error {
	for _, nameVal := range env {
		if isSensitive(nameVal.Name) {
			continue
		}
		err := session.Setenv(nameVal.Name, nameVal.Value)
		if err != nil {
			return fmt.Errorf("failed to set environment variable '%v': %v", nameVal.Name, err)
		}
	}
	return nil
}

// DialWithDeadline works around the case when net.DialWithTimeout
// succeeds, but key exchange hangs. Setting deadline on connection
// prevents this case from happening
func DialWithDeadline(network string, addr string, config *ssh.ClientConfig) (*ssh.Client, error) {
	conn, err := net.DialTimeout(network, addr, config.Timeout)
	if err != nil {
		return nil, err
	}
	if config.Timeout > 0 {
		conn.SetReadDeadline(time.Now().Add(config.Timeout))
	}
	c, chans, reqs, err := ssh.NewClientConn(conn, addr, config)
	if err != nil {
		return nil, err
	}
	if config.Timeout > 0 {
		conn.SetReadDeadline(time.Time{})
	}
	return ssh.NewClient(c, chans, reqs), nil
}

func (s *Runner) Run(ctx context.Context, dialInfo DialInfo, cmd []string, opts ...Option) error {
	o := makeOptions(dialInfo, opts...)

	// it can be stuck here
	client, err := DialWithDeadline("tcp", dialInfo.Address, o.config)
	if err != nil {
		return fmt.Errorf("failed to connect to '%v': %w", dialInfo.Address, err)
	}
	defer client.Close()

	// Quote each argument to handle spaces and special characters
	for i, arg := range cmd {
		cmd[i] = shellQuote(arg)
	}

	commandStr := strings.Join(cmd, " ")

	done := make(chan error, 1)
	go func() {
		select {
		case <-ctx.Done():
			err := client.Close()
			if err != nil {
				log.Printf("%v: failed to send close network client for '%v': %v", o.prefix, commandStr, err)
			}
		case <-done:
			log.Printf("%v: SSH session for command '%v' finished", o.prefix, commandStr)
		}
	}()

	// Create a session for running the command
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer session.Close()

	err = setEnvVars(session, o.env, o.isSensitiveEnvVar)
	if err != nil {
		return fmt.Errorf("failed to apply environment settings to '%v': %w", commandStr, err)
	}

	if err := attachStreams(session, o.attachIO); err != nil {
		return fmt.Errorf("failed to attach streams: %w", err)
	}

	err = session.Start(commandStr)
	if err != nil {
		return fmt.Errorf("%v: failed to start SSH session '%v': %w", o.prefix, commandStr, err)
	}

	err = session.Wait()
	done <- err
	return err
}
