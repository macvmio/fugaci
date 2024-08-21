package runtimeservice

import (
	"context"
	"encoding/json"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"
	"log"
)

type LoggingRuntimeService struct {
	runtimeapi.UnimplementedRuntimeServiceServer
	upstream      runtimeapi.RuntimeServiceServer
	logNamespaces []string
}

func NewLoggingRuntimeService(upstream runtimeapi.RuntimeServiceServer) *LoggingRuntimeService {
	return &LoggingRuntimeService{upstream: upstream, logNamespaces: []string{"jenkins"}}
}

func (s *LoggingRuntimeService) logRequestAndResponse(method string, req interface{}, res interface{}, err error) {
	reqJSON, _ := json.MarshalIndent(req, "", "  ")
	log.Printf("Request to %s:\n%s\n", method, string(reqJSON))
	if err != nil {
		log.Printf("Response from %s (error): %v\n", method, err)
	} else {
		resJSON, _ := json.MarshalIndent(res, "", "  ")
		log.Printf("Response from %s:\n%s\n", method, string(resJSON))
	}
}

func (s *LoggingRuntimeService) Version(ctx context.Context, req *runtimeapi.VersionRequest) (*runtimeapi.VersionResponse, error) {
	method := "Version"
	res, err := s.upstream.Version(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) RunPodSandbox(ctx context.Context, req *runtimeapi.RunPodSandboxRequest) (*runtimeapi.RunPodSandboxResponse, error) {
	method := "RunPodSandbox"
	res, err := s.upstream.RunPodSandbox(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) StopPodSandbox(ctx context.Context, req *runtimeapi.StopPodSandboxRequest) (*runtimeapi.StopPodSandboxResponse, error) {
	method := "StopPodSandbox"
	res, err := s.upstream.StopPodSandbox(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) RemovePodSandbox(ctx context.Context, req *runtimeapi.RemovePodSandboxRequest) (*runtimeapi.RemovePodSandboxResponse, error) {
	method := "RemovePodSandbox"
	res, err := s.upstream.RemovePodSandbox(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) PodSandboxStatus(ctx context.Context, req *runtimeapi.PodSandboxStatusRequest) (*runtimeapi.PodSandboxStatusResponse, error) {
	method := "PodSandboxStatus"
	res, err := s.upstream.PodSandboxStatus(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) ListPodSandbox(ctx context.Context, req *runtimeapi.ListPodSandboxRequest) (*runtimeapi.ListPodSandboxResponse, error) {
	//method := "ListPodSandbox"
	//s.logRequestAndResponse(method, req, nil, nil)
	res, err := s.upstream.ListPodSandbox(ctx, req)
	//s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) CreateContainer(ctx context.Context, req *runtimeapi.CreateContainerRequest) (*runtimeapi.CreateContainerResponse, error) {
	method := "CreateContainer"
	res, err := s.upstream.CreateContainer(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) StartContainer(ctx context.Context, req *runtimeapi.StartContainerRequest) (*runtimeapi.StartContainerResponse, error) {
	method := "StartContainer"
	res, err := s.upstream.StartContainer(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) StopContainer(ctx context.Context, req *runtimeapi.StopContainerRequest) (*runtimeapi.StopContainerResponse, error) {
	method := "StopContainer"
	res, err := s.upstream.StopContainer(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) RemoveContainer(ctx context.Context, req *runtimeapi.RemoveContainerRequest) (*runtimeapi.RemoveContainerResponse, error) {
	method := "RemoveContainer"
	res, err := s.upstream.RemoveContainer(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) ListContainers(ctx context.Context, req *runtimeapi.ListContainersRequest) (*runtimeapi.ListContainersResponse, error) {
	//method := "ListContainers"
	//s.logRequestAndResponse(method, req, nil, nil)
	res, err := s.upstream.ListContainers(ctx, req)
	//s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) ContainerStatus(ctx context.Context, req *runtimeapi.ContainerStatusRequest) (*runtimeapi.ContainerStatusResponse, error) {
	method := "ContainerStatus"
	res, err := s.upstream.ContainerStatus(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) UpdateContainerResources(ctx context.Context, req *runtimeapi.UpdateContainerResourcesRequest) (*runtimeapi.UpdateContainerResourcesResponse, error) {
	method := "UpdateContainerResources"
	res, err := s.upstream.UpdateContainerResources(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) ReopenContainerLog(ctx context.Context, req *runtimeapi.ReopenContainerLogRequest) (*runtimeapi.ReopenContainerLogResponse, error) {
	method := "ReopenContainerLog"
	res, err := s.upstream.ReopenContainerLog(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) ExecSync(ctx context.Context, req *runtimeapi.ExecSyncRequest) (*runtimeapi.ExecSyncResponse, error) {
	method := "ExecSync"
	res, err := s.upstream.ExecSync(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) Exec(ctx context.Context, req *runtimeapi.ExecRequest) (*runtimeapi.ExecResponse, error) {
	method := "Exec"
	res, err := s.upstream.Exec(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) Attach(ctx context.Context, req *runtimeapi.AttachRequest) (*runtimeapi.AttachResponse, error) {
	method := "Attach"
	res, err := s.upstream.Attach(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) PortForward(ctx context.Context, req *runtimeapi.PortForwardRequest) (*runtimeapi.PortForwardResponse, error) {
	method := "PortForward"
	res, err := s.upstream.PortForward(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) ContainerStats(ctx context.Context, req *runtimeapi.ContainerStatsRequest) (*runtimeapi.ContainerStatsResponse, error) {
	method := "ContainerStats"
	res, err := s.upstream.ContainerStats(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) ListContainerStats(ctx context.Context, req *runtimeapi.ListContainerStatsRequest) (*runtimeapi.ListContainerStatsResponse, error) {
	method := "ListContainerStats"
	res, err := s.upstream.ListContainerStats(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) PodSandboxStats(ctx context.Context, req *runtimeapi.PodSandboxStatsRequest) (*runtimeapi.PodSandboxStatsResponse, error) {
	method := "PodSandboxStats"
	res, err := s.upstream.PodSandboxStats(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) ListPodSandboxStats(ctx context.Context, req *runtimeapi.ListPodSandboxStatsRequest) (*runtimeapi.ListPodSandboxStatsResponse, error) {
	method := "ListPodSandboxStats"
	res, err := s.upstream.ListPodSandboxStats(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) UpdateRuntimeConfig(ctx context.Context, req *runtimeapi.UpdateRuntimeConfigRequest) (*runtimeapi.UpdateRuntimeConfigResponse, error) {
	method := "UpdateRuntimeConfig"
	res, err := s.upstream.UpdateRuntimeConfig(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) Status(ctx context.Context, req *runtimeapi.StatusRequest) (*runtimeapi.StatusResponse, error) {
	method := "Status"
	res, err := s.upstream.Status(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) CheckpointContainer(ctx context.Context, req *runtimeapi.CheckpointContainerRequest) (*runtimeapi.CheckpointContainerResponse, error) {
	method := "CheckpointContainer"
	res, err := s.upstream.CheckpointContainer(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) GetContainerEvents(req *runtimeapi.GetEventsRequest, srv runtimeapi.RuntimeService_GetContainerEventsServer) error {
	method := "GetContainerEvents"
	err := s.upstream.GetContainerEvents(req, srv)
	s.logRequestAndResponse(method, req, nil, err)
	return err
}

func (s *LoggingRuntimeService) ListMetricDescriptors(ctx context.Context, req *runtimeapi.ListMetricDescriptorsRequest) (*runtimeapi.ListMetricDescriptorsResponse, error) {
	method := "ListMetricDescriptors"
	res, err := s.upstream.ListMetricDescriptors(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) ListPodSandboxMetrics(ctx context.Context, req *runtimeapi.ListPodSandboxMetricsRequest) (*runtimeapi.ListPodSandboxMetricsResponse, error) {
	method := "ListPodSandboxMetrics"
	res, err := s.upstream.ListPodSandboxMetrics(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingRuntimeService) RuntimeConfig(ctx context.Context, req *runtimeapi.RuntimeConfigRequest) (*runtimeapi.RuntimeConfigResponse, error) {
	method := "RuntimeConfig"
	res, err := s.upstream.RuntimeConfig(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}
