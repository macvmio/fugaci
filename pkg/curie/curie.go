package curie

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	v1 "k8s.io/api/core/v1"
	"log"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Virtualization struct {
	curieBinaryPath string
}

func NewVirtualization(curieBinaryPath string) *Virtualization {
	return &Virtualization{
		curieBinaryPath: curieBinaryPath,
	}
}

var ErrNotExists = errors.New("not exists")

func (s *Virtualization) Create(ctx context.Context, pod v1.Pod, containerIndex int) (containerID string, err error) {
	containerSpec := pod.Spec.Containers[containerIndex]
	name := pod.Namespace + "-" + pod.Name + "-" + containerSpec.Name
	if len(containerSpec.Name) == 0 {
		return "", errors.New("empty name")
	}
	if len(containerSpec.Image) == 0 {
		return "", errors.New("empty image")
	}
	args := []string{"create", containerSpec.Image, "--name", name}
	cmd := exec.CommandContext(ctx, s.curieBinaryPath, args...)
	log.Print(cmd)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to create container: %w", err)
	}
	strOut := strings.TrimSpace(string(out))
	log.Printf("output of '%v' command: %v", cmd, strOut)
	return strOut, nil
}

func (s *Virtualization) Start(ctx context.Context, containerID string) (runCommand *exec.Cmd, err error) {
	args := []string{"start", containerID}
	cmd := exec.CommandContext(ctx, s.curieBinaryPath, args...)
	return cmd, cmd.Start()
}

func (s *Virtualization) Stop(ctx context.Context, containerRunCmd *exec.Cmd) error {
	if containerRunCmd == nil {
		return nil
	}

	// Send the interrupt signal to the process
	err := containerRunCmd.Process.Signal(os.Interrupt)
	if errors.Is(err, os.ErrProcessDone) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("unable to send os.Interrupt: %w", err)
	}

	// Create a channel to wait for the process to exit
	done := make(chan error, 1)
	go func() {
		done <- containerRunCmd.Wait()
	}()

	// Use a select statement to wait for either the process to exit or the timeout
	select {
	case <-time.After(5 * time.Second):
		return fmt.Errorf("process did not exit within 5 seconds")
	case err := <-done:
		if err != nil {
			return fmt.Errorf("process exited with error: %w", err)
		}
		log.Printf("container '%s' stopped successfully", containerRunCmd)
		return nil
	}
}

func (s *Virtualization) Destroy(ctx context.Context, containerID string) error {
	err := exec.CommandContext(ctx, s.curieBinaryPath, "rm", containerID).Run()
	log.Printf("removed container '%v': err=%v", containerID, err)
	return err
}

// Inspect runs the inspect command on the specified container and returns the inspection result.
func (s *Virtualization) Inspect(ctx context.Context, containerID string) (*InspectResponse, error) {
	// Execute the "inspect" command and capture its output.
	var stdout bytes.Buffer
	cmd := exec.CommandContext(ctx, s.curieBinaryPath, "inspect", containerID, "--format", "json")
	cmd.Stdout = &stdout // Capture standard output
	cmd.Stderr = &stdout // Optionally, capture standard error as well for detailed error messages

	if err := cmd.Run(); err != nil {
		// Return more information by including the output of the command (if any) in the error message.
		if strings.Contains(stdout.String(), "Cannot find the container") {
			return nil, ErrNotExists
		}
		return nil, fmt.Errorf("failed to execute inspect command: %v, output: %s", err, stdout.String())
	}

	// Parse JSON output into InspectResponse struct.
	var response InspectResponse
	if err := json.Unmarshal(stdout.Bytes(), &response); err != nil {
		// If parsing fails, return an appropriate error.
		return nil, fmt.Errorf("failed to parse inspect output: %v", err)
	}

	return &response, nil
}

// IP Returns valid IP address or error
func (s *Virtualization) IP(ctx context.Context, containerID string) (net.IP, error) {
	r, err := s.Inspect(ctx, containerID)
	if err != nil {
		return nil, err
	}
	if len(r.Arp) == 0 {
		return nil, errors.New("no arp found")
	}
	ip := net.ParseIP(r.Arp[0].IP)
	if ip == nil {
		return nil, fmt.Errorf("invalid ip address: %v", ip)
	}
	return ip, nil
}

func (s *Virtualization) Exists(ctx context.Context, containerID string) (bool, error) {
	_, err := s.Inspect(ctx, containerID)
	if errors.Is(err, ErrNotExists) {
		return false, nil
	}
	return err == nil, err
}
