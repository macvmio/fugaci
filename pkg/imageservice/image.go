package imageservice

import (
	"context"
	"sync"
	"time"

	runtimeapi "k8s.io/cri-api/pkg/apis/runtime/v1"
)

type InMemoryImageService struct {
	runtimeapi.UnimplementedImageServiceServer

	mu     sync.Mutex
	images map[string]string
}

func NewInMemoryImageService() *InMemoryImageService {
	return &InMemoryImageService{
		images: make(map[string]string),
	}
}

func (s *InMemoryImageService) ListImages(ctx context.Context, req *runtimeapi.ListImagesRequest) (*runtimeapi.ListImagesResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var images []*runtimeapi.Image
	for id, repoTag := range s.images {
		images = append(images, &runtimeapi.Image{
			Id:       id,
			RepoTags: []string{repoTag},
		})
	}
	return &runtimeapi.ListImagesResponse{Images: images}, nil
}

func (s *InMemoryImageService) ImageStatus(ctx context.Context, req *runtimeapi.ImageStatusRequest) (*runtimeapi.ImageStatusResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	imageID := req.Image.Image
	repoTag, exists := s.images[imageID]
	if !exists {
		return &runtimeapi.ImageStatusResponse{
			Image: nil,
		}, nil
	}
	return &runtimeapi.ImageStatusResponse{
		Image: &runtimeapi.Image{
			Id:       req.Image.Image,
			RepoTags: []string{repoTag},
			Size_:    1,
		},
	}, nil
}

func (s *InMemoryImageService) PullImage(ctx context.Context, req *runtimeapi.PullImageRequest) (*runtimeapi.PullImageResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	imageID := req.Image.Image
	s.images[imageID] = imageID
	return &runtimeapi.PullImageResponse{ImageRef: imageID}, nil
}

func (s *InMemoryImageService) RemoveImage(ctx context.Context, req *runtimeapi.RemoveImageRequest) (*runtimeapi.RemoveImageResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, repoTag := range s.images {
		if repoTag == req.Image.Image {
			delete(s.images, id)
			break
		}
	}
	return &runtimeapi.RemoveImageResponse{}, nil
}

func (s *InMemoryImageService) ImageFsInfo(ctx context.Context, req *runtimeapi.ImageFsInfoRequest) (*runtimeapi.ImageFsInfoResponse, error) {
	// Simulate ImageFsInfo by returning a fixed response
	return &runtimeapi.ImageFsInfoResponse{
		ImageFilesystems: []*runtimeapi.FilesystemUsage{
			{
				Timestamp:  time.Now().UnixNano(),
				FsId:       &runtimeapi.FilesystemIdentifier{Mountpoint: "/tmp/images"},
				UsedBytes:  &runtimeapi.UInt64Value{Value: 1024},
				InodesUsed: &runtimeapi.UInt64Value{Value: 1},
			},
		},
		ContainerFilesystems: []*runtimeapi.FilesystemUsage{
			{
				Timestamp:  time.Now().UnixNano(),
				FsId:       &runtimeapi.FilesystemIdentifier{Mountpoint: "/tmp/containers"},
				UsedBytes:  &runtimeapi.UInt64Value{Value: 1024},
				InodesUsed: &runtimeapi.UInt64Value{Value: 1},
			},
		},
	}, nil
}
