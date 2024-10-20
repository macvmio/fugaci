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
	"syscall"
	"time"
)

type Virtualization struct {
	curieBinaryPath   string
	curieDataRootPath string
}

func NewVirtualization(curieBinaryPath string, curieDataRootPath string) *Virtualization {
	return &Virtualization{
		curieBinaryPath:   curieBinaryPath,
		curieDataRootPath: curieDataRootPath,
	}
}

var ErrNotExists = errors.New("not exists")

func (s *Virtualization) prepareEnv(cmd *exec.Cmd) {
	cmd.Env = append(os.Environ(), "CURIE_DATA_ROOT="+s.curieDataRootPath)
}

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
	s.prepareEnv(cmd)
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
	args := []string{"start", "--no-window", containerID}
	cmd := exec.CommandContext(ctx, s.curieBinaryPath, args...)
	s.prepareEnv(cmd)
	// TODO: Use command cancel here!!
	//cmd.Cancel = func() error {
	//	s.Stop()
	//}
	return cmd, cmd.Start()
}

func (s *Virtualization) Stop(ctx context.Context, containerRunCmdPid int) error {
	if containerRunCmdPid == 0 {
		return nil
	}

	// Send the interrupt signal to the process
	log.Printf("sending SIGTERM to process %d", containerRunCmdPid)
	containerRunProcess, err := os.FindProcess(containerRunCmdPid)
	if err != nil {
		return fmt.Errorf("could not find process to stop: %w", err)
	}
	err = containerRunProcess.Signal(syscall.SIGTERM)
	if errors.Is(err, os.ErrProcessDone) {
		log.Printf("process %d terminated", containerRunCmdPid)
		return nil
	}
	if err != nil {
		return fmt.Errorf("unable to send SIGTERM signal: %w", err)
	}
	start := time.Now()

	for {
		select {
		case <-ctx.Done():
			// Context has timed out
			return fmt.Errorf("process still did not exit after %v", time.Since(start).Round(time.Second))
		default:
			// Check if the process is still running
			p, err := os.FindProcess(containerRunCmdPid)
			if err == nil {
				// Probe the process to check if it's really alive
				err := p.Signal(syscall.Signal(0))
				_ = p.Release()
				// NOTE: Process might be in defunct state here, so this needs to be handled at higher layers
				if errors.Is(err, os.ErrProcessDone) {
					log.Printf("process %d exited after %v\n", containerRunCmdPid, time.Since(start))
					return nil
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func (s *Virtualization) Destroy(ctx context.Context, containerID string) error {
	cmd := exec.CommandContext(ctx, s.curieBinaryPath, "rm", containerID)
	s.prepareEnv(cmd)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout // Capture standard output
	err := cmd.Run()
	if err != nil {
		log.Printf("error while calling cmd='%s', err=%v", cmd.String(), err)
		if err2 := s.isKnownError(stdout.String()); errors.Is(err2, ErrNotExists) {
			log.Printf("destroy container '%v': no longer exists", containerID)
			return nil
		}
	}
	log.Printf("destroy container '%v': err=%v", containerID, err)
	return err
}

func (s *Virtualization) isKnownError(output string) (err error) {
	if strings.Contains(output, "Cannot find the container") {
		return ErrNotExists
	}
	return nil
}

// Inspect runs the inspect command on the specified container and returns the inspection result.
func (s *Virtualization) Inspect(ctx context.Context, containerID string) (*InspectResponse, error) {
	// Execute the "inspect" command and capture its output.
	cmd := exec.CommandContext(ctx, s.curieBinaryPath, "inspect", containerID, "--format", "json")
	s.prepareEnv(cmd)

	var stdout bytes.Buffer
	cmd.Stdout = &stdout // Capture standard output
	cmd.Stderr = &stdout // Optionally, capture standard error as well for detailed error messages

	if err := cmd.Run(); err != nil {
		if err2 := s.isKnownError(stdout.String()); err2 != nil {
			return nil, err2
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
