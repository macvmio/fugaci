package sshrunner

import (
	"context"
	"fmt"
	"github.com/virtual-kubelet/virtual-kubelet/node/api"
	"golang.org/x/crypto/ssh"
	"log"
	"strings"
)

type Runner struct {
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
}

func AttachStreams(session *ssh.Session, attach api.AttachIO) error {
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

func SetEnvVars(session *ssh.Session, env map[string]string, isSensitive func(name string) bool) error {
	for name, value := range env {
		if isSensitive(name) {
			continue
		}
		err := session.Setenv(name, value)
		if err != nil {
			return fmt.Errorf("failed to set environment variable '%v': %v", name, err)
		}
	}
	return nil
}

func (s *Runner) Run(ctx context.Context, address string, config *ssh.ClientConfig, cmd []string, preConnection ...func(session *ssh.Session) error) error {
	client, err := ssh.Dial("tcp", address, config)
	if err != nil {
		return fmt.Errorf("failed to connect to '%v': %w", address, err)
	}
	defer client.Close()

	// Create a session for running the command
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer session.Close()

	// Quote each argument to handle spaces and special characters
	for i, arg := range cmd {
		cmd[i] = shellQuote(arg)
	}

	// Join the command and arguments into a single string
	commandStr := strings.Join(cmd, " ")

	for _, pr := range preConnection {
		if err := pr(session); err != nil {
			return fmt.Errorf("failed to apply pre conenction settings to '%v': %w", commandStr, err)
		}
	}
	defer func() {
		log.Printf("SSH session for command: '%v' has finished", commandStr)
	}()
	return session.Run(commandStr)
}
