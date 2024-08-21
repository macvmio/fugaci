package runtimeservice

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sync"
	"time"
)

type InMemoryRuntimeService struct {
	runtimeapi.UnimplementedRuntimeServiceServer

	mu                sync.Mutex
	podSandboxes      map[string]*runtimeapi.PodSandbox
	podSandboxConfigs map[string]*runtimeapi.PodSandboxConfig
	containers        map[string]*runtimeapi.Container
	containerConfigs  map[string]*runtimeapi.ContainerConfig
	imageService      runtimeapi.ImageServiceServer
}

func NewInMemoryRuntimeService(imageService runtimeapi.ImageServiceServer) *InMemoryRuntimeService {
	return &InMemoryRuntimeService{
		podSandboxes:      make(map[string]*runtimeapi.PodSandbox),
		podSandboxConfigs: make(map[string]*runtimeapi.PodSandboxConfig),
		containers:        make(map[string]*runtimeapi.Container),
		containerConfigs:  map[string]*runtimeapi.ContainerConfig{},
		imageService:      imageService,
	}
}

func ptr[V any](x V) *V {
	return &x
}

func (s *InMemoryRuntimeService) Version(ctx context.Context, req *runtimeapi.VersionRequest) (*runtimeapi.VersionResponse, error) {
	return &runtimeapi.VersionResponse{
		Version:           "0.1.0",
		RuntimeName:       "InMemoryRuntime",
		RuntimeVersion:    "0.1.0",
		RuntimeApiVersion: "0.1.0",
	}, nil
}

func (s *InMemoryRuntimeService) RunPodSandbox(ctx context.Context, req *runtimeapi.RunPodSandboxRequest) (*runtimeapi.RunPodSandboxResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if req.Config == nil {
		return nil, status.Error(codes.InvalidArgument, "pod sandbox config is required")
	}
	podSandboxID := req.Config.GetMetadata().GetUid()
	s.podSandboxes[podSandboxID] = &runtimeapi.PodSandbox{
		Id: podSandboxID,
		Metadata: &runtimeapi.PodSandboxMetadata{
			Name:      req.Config.GetMetadata().GetName(),
			Uid:       req.Config.GetMetadata().GetUid(),
			Namespace: req.Config.GetMetadata().GetNamespace(),
			Attempt:   req.Config.GetMetadata().GetAttempt(),
		},
		CreatedAt: time.Now().UnixNano(),
		State:     runtimeapi.PodSandboxState_SANDBOX_READY,
		Labels:    req.Config.GetLabels(),
	}
	s.podSandboxConfigs[podSandboxID] = req.Config
	return &runtimeapi.RunPodSandboxResponse{PodSandboxId: podSandboxID}, nil
}

func (s *InMemoryRuntimeService) StopPodSandbox(ctx context.Context, req *runtimeapi.StopPodSandboxRequest) (*runtimeapi.StopPodSandboxResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sandbox, exists := s.podSandboxes[req.PodSandboxId]; exists {
		sandbox.State = runtimeapi.PodSandboxState_SANDBOX_NOTREADY
	}
	return &runtimeapi.StopPodSandboxResponse{}, nil
}

func (s *InMemoryRuntimeService) RemovePodSandbox(ctx context.Context, req *runtimeapi.RemovePodSandboxRequest) (*runtimeapi.RemovePodSandboxResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.podSandboxes, req.PodSandboxId)
	delete(s.podSandboxConfigs, req.PodSandboxId)
	return &runtimeapi.RemovePodSandboxResponse{}, nil
}

func (s *InMemoryRuntimeService) PodSandboxStatus(ctx context.Context, req *runtimeapi.PodSandboxStatusRequest) (*runtimeapi.PodSandboxStatusResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sandbox, exists := s.podSandboxes[req.PodSandboxId]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "pod sandbox %s not found", req.PodSandboxId)
	}
	return &runtimeapi.PodSandboxStatusResponse{
		Status: &runtimeapi.PodSandboxStatus{
			Id:        sandbox.Id,
			Metadata:  sandbox.Metadata,
			CreatedAt: sandbox.CreatedAt,
			Network:   nil,
			State:     sandbox.State,
			Labels:    sandbox.Labels,
			// TODO: Container Statuses
		},
	}, nil
}

func (s *InMemoryRuntimeService) ListPodSandbox(ctx context.Context, req *runtimeapi.ListPodSandboxRequest) (*runtimeapi.ListPodSandboxResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var items []*runtimeapi.PodSandbox
	for _, sandbox := range s.podSandboxes {
		items = append(items, sandbox)
	}
	return &runtimeapi.ListPodSandboxResponse{Items: items}, nil
}

// generateUUID generates a random UUID according to RFC 4122
func generateUUID() (string, error) {
	b := make([]byte, 16)

	// Read random bytes into b
	_, err := rand.Read(b)
	if err != nil {
		return "", err
	}

	// Adjust certain bits according to version 4 UUID spec
	b[6] = (b[6] & 0x0f) | 0x40 // Version 4
	b[8] = (b[8] & 0x3f) | 0x80 // Variant 10

	// Format the UUID string to RFC 4122 standard
	uuid := fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
	return uuid, nil
}

func (s *InMemoryRuntimeService) CreateContainer(ctx context.Context, req *runtimeapi.CreateContainerRequest) (*runtimeapi.CreateContainerResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	uuid, err := generateUUID()
	if err != nil {
		return nil, status.Error(codes.Internal, "unable to generate uuid")
	}
	containerID := fmt.Sprintf("%s-%s", req.GetConfig().GetMetadata().GetName(), uuid)

	s.containers[containerID] = &runtimeapi.Container{
		Id:           containerID,
		PodSandboxId: req.PodSandboxId,
		Image:        req.Config.GetImage(),
		ImageRef:     containerID, // TODO
		CreatedAt:    time.Now().UnixNano(),
		ImageId:      containerID, // Identifier on the node
		Metadata: &runtimeapi.ContainerMetadata{
			Name:    req.Config.GetMetadata().GetName(),
			Attempt: req.Config.GetMetadata().GetAttempt(),
		},
		State:  runtimeapi.ContainerState_CONTAINER_RUNNING,
		Labels: req.Config.GetLabels(),
	}
	s.containerConfigs[containerID] = req.Config
	//err := s.openLogFile(ctx, containerID, req.PodSandboxId)
	//if err != nil {
	//	return nil, status.Errorf(codes.Internal, "unable to open log, err=%v", err)
	//}
	return &runtimeapi.CreateContainerResponse{ContainerId: containerID}, nil
}

func (s *InMemoryRuntimeService) StartContainer(ctx context.Context, req *runtimeapi.StartContainerRequest) (*runtimeapi.StartContainerResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if container, exists := s.containers[req.ContainerId]; exists {
		container.State = runtimeapi.ContainerState_CONTAINER_RUNNING
	}
	return &runtimeapi.StartContainerResponse{}, nil
}

func (s *InMemoryRuntimeService) StopContainer(ctx context.Context, req *runtimeapi.StopContainerRequest) (*runtimeapi.StopContainerResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if container, exists := s.containers[req.ContainerId]; exists {
		container.State = runtimeapi.ContainerState_CONTAINER_EXITED
	}
	return &runtimeapi.StopContainerResponse{}, nil
}

func (s *InMemoryRuntimeService) RemoveContainer(ctx context.Context, req *runtimeapi.RemoveContainerRequest) (*runtimeapi.RemoveContainerResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.containers, req.ContainerId)
	delete(s.containerConfigs, req.ContainerId)
	return &runtimeapi.RemoveContainerResponse{}, nil
}

func (s *InMemoryRuntimeService) ListContainers(ctx context.Context, req *runtimeapi.ListContainersRequest) (*runtimeapi.ListContainersResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var containers []*runtimeapi.Container
	for _, container := range s.containers {
		containers = append(containers, container)
	}
	return &runtimeapi.ListContainersResponse{Containers: containers}, nil
}

func (s *InMemoryRuntimeService) ContainerStatus(ctx context.Context, req *runtimeapi.ContainerStatusRequest) (*runtimeapi.ContainerStatusResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	container, exists := s.containers[req.ContainerId]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "container %s not found", req.ContainerId)
	}
	return &runtimeapi.ContainerStatusResponse{
		Status: &runtimeapi.ContainerStatus{
			Id:        container.Id,
			CreatedAt: container.CreatedAt,
			Metadata:  container.Metadata,
			Image:     container.Image,
			ImageRef:  container.ImageRef,
			State:     container.State,
			Labels:    container.Labels,
		},
	}, nil
}

func (s *InMemoryRuntimeService) openLogFile(ctx context.Context, containerID, podSandboxID string) error {
	containerConfig, exists := s.containerConfigs[containerID]
	if !exists {
		return fmt.Errorf("containerID '%s' does not exist", containerID)
	}
	sandboxConfig, exists := s.podSandboxConfigs[podSandboxID]
	if !exists {
		return fmt.Errorf("podSandboxID '%s' does not exist", podSandboxID)
	}

	if sandboxConfig.GetLogDirectory() == "" || containerConfig.GetLogPath() == "" {
		return nil
	}

	fullpath := path.Join(sandboxConfig.GetLogDirectory(), containerConfig.GetLogPath())

	log.Printf("Creating file at fullpath %s\n", fullpath)

	err := os.MkdirAll(filepath.Dir(fullpath), 0o777)
	if err != nil {
		return err
	}

	f, err := os.Create(fullpath)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "hello")
	return err
}

func (s *InMemoryRuntimeService) UpdateContainerResources(ctx context.Context, req *runtimeapi.UpdateContainerResourcesRequest) (*runtimeapi.UpdateContainerResourcesResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	container, exists := s.containers[req.ContainerId]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "container %s not found", req.ContainerId)
	}
	container.Annotations = req.Annotations
	return &runtimeapi.UpdateContainerResourcesResponse{}, nil
}

func (s *InMemoryRuntimeService) ReopenContainerLog(ctx context.Context, req *runtimeapi.ReopenContainerLogRequest) (*runtimeapi.ReopenContainerLogResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	container, exists := s.containers[req.ContainerId]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "container %s not found", req.ContainerId)
	}

	err := s.openLogFile(ctx, container.Id, container.PodSandboxId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "unable to open log, err=%v", err)
	}
	return &runtimeapi.ReopenContainerLogResponse{}, nil
}

func (s *InMemoryRuntimeService) ExecSync(ctx context.Context, req *runtimeapi.ExecSyncRequest) (*runtimeapi.ExecSyncResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.containers[req.ContainerId]; !exists {
		return nil, status.Errorf(codes.NotFound, "container %s not found", req.ContainerId)
	}

	ctx2, cancel := context.WithTimeout(ctx, time.Second*time.Duration(req.GetTimeout()))
	defer cancel()

	lxcPrefix := []string{"lxc", "exec", "test2", "--"}
	extCmd := append(lxcPrefix, req.Cmd...)
	cmd := exec.CommandContext(ctx2, extCmd[0], extCmd[1:]...)

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	stdout := stdoutBuf.Bytes()
	stderr := stderrBuf.Bytes()

	var exitCode int32
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			exitCode = int32(exitError.ExitCode())
		}
	} else {
		exitCode = 0
	}

	return &runtimeapi.ExecSyncResponse{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
	}, nil
}

func (s *InMemoryRuntimeService) Exec(ctx context.Context, req *runtimeapi.ExecRequest) (*runtimeapi.ExecResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.containers[req.ContainerId]; !exists {
		return nil, status.Errorf(codes.NotFound, "container %s not found", req.ContainerId)
	}
	return &runtimeapi.ExecResponse{
		Url: "http://localhost:8080/exec",
	}, nil
}

func (s *InMemoryRuntimeService) Attach(ctx context.Context, req *runtimeapi.AttachRequest) (*runtimeapi.AttachResponse, error) {
	// Simulate Attach by returning a fixed URL
	return &runtimeapi.AttachResponse{
		Url: "http://localhost:8080/attach",
	}, nil
}

func (s *InMemoryRuntimeService) PortForward(ctx context.Context, req *runtimeapi.PortForwardRequest) (*runtimeapi.PortForwardResponse, error) {
	// Simulate PortForward by returning a fixed URL
	return &runtimeapi.PortForwardResponse{
		Url: "http://localhost:8080/portforward",
	}, nil
}

func (s *InMemoryRuntimeService) ContainerStats(ctx context.Context, req *runtimeapi.ContainerStatsRequest) (*runtimeapi.ContainerStatsResponse, error) {
	// Simulate ContainerStats by returning a fixed response
	container, exists := s.containers[req.ContainerId]
	if !exists {
		return nil, status.Errorf(codes.NotFound, "container %s not found", req.ContainerId)
	}

	return &runtimeapi.ContainerStatsResponse{
		Stats: &runtimeapi.ContainerStats{
			Attributes: &runtimeapi.ContainerAttributes{
				Id:          req.ContainerId,
				Metadata:    container.Metadata,
				Labels:      container.Labels,
				Annotations: container.Annotations,
			},
			Cpu: &runtimeapi.CpuUsage{
				Timestamp:            time.Now().Unix(),
				UsageCoreNanoSeconds: &runtimeapi.UInt64Value{Value: 123},
				UsageNanoCores:       &runtimeapi.UInt64Value{Value: 10},
			},
			Memory: &runtimeapi.MemoryUsage{
				Timestamp: time.Now().Unix(),
			},
		},
	}, nil
}

func (s *InMemoryRuntimeService) ListContainerStats(ctx context.Context, req *runtimeapi.ListContainerStatsRequest) (*runtimeapi.ListContainerStatsResponse, error) {
	// Simulate ListContainerStats by returning a fixed response
	var containerStats []*runtimeapi.ContainerStats
	for id := range s.containers {
		containerStats = append(containerStats, &runtimeapi.ContainerStats{
			Attributes: &runtimeapi.ContainerAttributes{
				Id: id,
			},
		})
	}
	return &runtimeapi.ListContainerStatsResponse{Stats: containerStats}, nil
}

func (s *InMemoryRuntimeService) PodSandboxStats(ctx context.Context, req *runtimeapi.PodSandboxStatsRequest) (*runtimeapi.PodSandboxStatsResponse, error) {
	// Simulate PodSandboxStats by returning a fixed response
	return &runtimeapi.PodSandboxStatsResponse{
		Stats: &runtimeapi.PodSandboxStats{
			Attributes: &runtimeapi.PodSandboxAttributes{
				Id: req.PodSandboxId,
			},
		},
	}, nil
}

func (s *InMemoryRuntimeService) ListPodSandboxStats(ctx context.Context, req *runtimeapi.ListPodSandboxStatsRequest) (*runtimeapi.ListPodSandboxStatsResponse, error) {
	// Simulate ListPodSandboxStats by returning a fixed response
	var podSandboxStats []*runtimeapi.PodSandboxStats
	for id := range s.podSandboxes {
		podSandboxStats = append(podSandboxStats, &runtimeapi.PodSandboxStats{
			Attributes: &runtimeapi.PodSandboxAttributes{
				Id: id,
			},
		})
	}
	return &runtimeapi.ListPodSandboxStatsResponse{Stats: podSandboxStats}, nil
}

func (s *InMemoryRuntimeService) UpdateRuntimeConfig(ctx context.Context, req *runtimeapi.UpdateRuntimeConfigRequest) (*runtimeapi.UpdateRuntimeConfigResponse, error) {
	// In-memory implementation does not actually update runtime config
	return &runtimeapi.UpdateRuntimeConfigResponse{}, nil
}

func (s *InMemoryRuntimeService) Status(ctx context.Context, req *runtimeapi.StatusRequest) (*runtimeapi.StatusResponse, error) {
	// Simulate Status by returning a fixed response
	return &runtimeapi.StatusResponse{
		Status: &runtimeapi.RuntimeStatus{
			Conditions: []*runtimeapi.RuntimeCondition{
				{
					Type:   "RuntimeReady",
					Status: true,
				},
				{
					Type:   "NetworkReady",
					Status: true,
				},
			},
		},
	}, nil
}

func (s *InMemoryRuntimeService) RuntimeConfig(ctx context.Context, req *runtimeapi.RuntimeConfigRequest) (*runtimeapi.RuntimeConfigResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Return the current runtime configuration
	return &runtimeapi.RuntimeConfigResponse{
		Linux: &runtimeapi.LinuxRuntimeConfiguration{
			CgroupDriver: runtimeapi.CgroupDriver_SYSTEMD,
		},
	}, nil
}
