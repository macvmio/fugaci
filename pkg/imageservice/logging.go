package imageservice

import (
	"context"
	"encoding/json"
	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"
	"log"
)

type LoggingImageService struct {
	runtimeapi.UnimplementedImageServiceServer
	upstream runtimeapi.ImageServiceServer
}

func NewLoggingImageService(upstream runtimeapi.ImageServiceServer) *LoggingImageService {
	return &LoggingImageService{upstream: upstream}
}

func (s *LoggingImageService) logRequestAndResponse(method string, req interface{}, res interface{}, err error) {
	reqJSON, _ := json.MarshalIndent(req, "", "  ")
	log.Printf("Request to %s:\n%s\n", method, string(reqJSON))
	if err != nil {
		log.Printf("Response from %s (error): %v\n", method, err)
	} else {
		resJSON, err := json.MarshalIndent(res, "", "  ")
		if err != nil {
			log.Printf("Response from %s (json error): %v\n", method, err)
		}
		log.Printf("Response from %s:\n%s\n", method, string(resJSON))
	}
}

func (s *LoggingImageService) ListImages(ctx context.Context, req *runtimeapi.ListImagesRequest) (*runtimeapi.ListImagesResponse, error) {
	method := "ListImages"
	res, err := s.upstream.ListImages(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingImageService) ImageStatus(ctx context.Context, req *runtimeapi.ImageStatusRequest) (*runtimeapi.ImageStatusResponse, error) {
	method := "ImageStatus"
	res, err := s.upstream.ImageStatus(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingImageService) PullImage(ctx context.Context, req *runtimeapi.PullImageRequest) (*runtimeapi.PullImageResponse, error) {
	method := "PullImage"
	res, err := s.upstream.PullImage(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingImageService) RemoveImage(ctx context.Context, req *runtimeapi.RemoveImageRequest) (*runtimeapi.RemoveImageResponse, error) {
	method := "RemoveImage"
	res, err := s.upstream.RemoveImage(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}

func (s *LoggingImageService) ImageFsInfo(ctx context.Context, req *runtimeapi.ImageFsInfoRequest) (*runtimeapi.ImageFsInfoResponse, error) {
	method := "ImageFsInfo"
	res, err := s.upstream.ImageFsInfo(ctx, req)
	s.logRequestAndResponse(method, req, res, err)
	return res, err
}
