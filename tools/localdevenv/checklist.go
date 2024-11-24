package main

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Check Define a type for the checks
type Check struct {
	name string
	fn   func() error
}

type Checklist struct {
	checks []Check
}

func (c *Checklist) addCheck(name string, f func() error) {
	c.checks = append(c.checks, Check{name, f})
}

func (c *Checklist) addCheckForEnvVariable(envVarName string) {
	c.addCheck(fmt.Sprintf("%s environment variable", envVarName), checkEnvVarExists(envVarName))
}

func (c *Checklist) addExecutableInstallationCheck(executableName string) {
	c.addCheck(fmt.Sprintf("%s is installed", executableName), checkBinaryExists(executableName))
}

func (c *Checklist) verify() bool {
	allPassed := true
	for _, c := range c.checks {
		err := c.fn()
		if err != nil {
			fmt.Printf("❌ %s: %v\n", c.name, err)
			allPassed = false
		} else {
			fmt.Printf("✅ %s\n", c.name)
		}
	}
	return allPassed
}

func checkEnvVarExists(envVarName string) func() error {
	return func() error {
		if os.Getenv(envVarName) == "" {
			return fmt.Errorf("%s environment variable is not set", envVarName)
		}
		return nil
	}
}

func checkBinaryExists(binary string) func() error {
	return func() error {
		if _, err := exec.LookPath(binary); err != nil {
			return fmt.Errorf("'%s' is not installed", binary)
		}
		return nil
	}
}

func checkSSHConnectivity(sshNodeName string) func() error {
	return func() error {
		cmd := exec.Command("ssh", "-q", sshNodeName, "exit")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("cannot connect to '%s' via SSH", sshNodeName)
		}
		return nil
	}
}

func checkBinaryExistsRemotely(sshNodeName string) func() error {
	return func() error {
		cmd := exec.Command("ssh", sshNodeName, "PATH=/usr/local/bin:$PATH command -v curie")
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("'curie' is not installed or not in PATH on '%s'", sshNodeName)
		}
		return nil
	}
}

func checkDockerInstallation() error {
	// Check if 'docker' is installed
	if _, err := exec.LookPath("docker"); err != nil {
		return fmt.Errorf("docker is not installed")
	}

	// Get 'docker version'
	cmd := exec.Command("docker", "version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error getting Docker version: %v\nOutput: %s", err, output)
	}
	// Optionally, print Docker version
	// fmt.Printf("Docker version:\n%s\n", output)

	// Check if 'docker compose' or 'docker-compose' is available
	// Try 'docker compose version'
	cmd = exec.Command("docker", "compose", "version")
	output, err = cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("Docker Compose is not installed.\nOutput: %s", output)
	}

	return nil
}

func checkIPAddressBehindSSHMatchesEnvVar(sshNodeName string, envVarName string) error {
	alias := sshNodeName
	ipFromEnvVar := os.Getenv(envVarName)
	if ipFromEnvVar == "" {
		return fmt.Errorf("%s environment variable is not set", envVarName)
	}

	// Run 'ssh -v m1 exit' and capture stderr
	cmd := exec.Command("ssh", "-v", alias, "exit")
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %v", err)
	}

	// Start the command
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start ssh command: %v", err)
	}

	// Read stderr output
	scanner := bufio.NewScanner(stderrPipe)
	var connectedIP string
	for scanner.Scan() {
		line := scanner.Text()
		// Look for lines like: "debug1: Connecting to example.com [192.168.1.100] port 22."
		if strings.Contains(line, "Connecting to") && strings.Contains(line, "port") {
			// Extract the IP address from the line
			parts := strings.Split(line, "[")
			if len(parts) > 1 {
				ipPart := parts[1]
				ipParts := strings.Split(ipPart, "]")
				if len(ipParts) > 0 {
					connectedIP = ipParts[0]
					break
				}
			}
		}
	}

	// Wait for the command to finish
	if err := cmd.Wait(); err != nil {
		// Ignore exit errors since we're only interested in the connection info
	}

	if connectedIP == "" {
		return fmt.Errorf("could not extract IP address from ssh logs")
	}

	if connectedIP != ipFromEnvVar {
		return fmt.Errorf("%s (%s) does not match SSH '%s' IP (%s)",
			envVarName, ipFromEnvVar, connectedIP, sshNodeName)
	}

	return nil
}

func checkPortReachable(ip string, port string) error {
	address := net.JoinHostPort(ip, port)
	timeout := 5 * time.Second
	// Try to establish a TCP connection
	conn, err := net.DialTimeout("tcp", address, timeout)
	if err != nil {
		return fmt.Errorf("port %s on %s is not reachable: %v", port, ip, err)
	}
	defer conn.Close()

	return nil
}
