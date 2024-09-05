package fugaci

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/mobileinf/geranos/pkg/dirimage"
	"github.com/mobileinf/geranos/pkg/layout"
	v1 "k8s.io/api/core/v1"
	"log"
)

type GeranosPuller struct {
	imagesPath string
}

func NewGeranosPuller(imagesPath string) *GeranosPuller {
	return &GeranosPuller{imagesPath: imagesPath}
}

// ConvertBytesToGB converts bytes to gigabytes
func convertBytesToGB(bytes int64) float64 {
	const bytesInGB = 1024 * 1024 * 1024
	return float64(bytes) / float64(bytesInGB)
}

// FormatProgress formats the progress update into a string like "pulling 5% (3GB/20GB)"
func formatProgress(progress dirimage.ProgressUpdate) string {
	if progress.BytesTotal == 0 {
		return "pulling 0% (0GB/0GB)" // Handle case where total is 0 to avoid division by zero
	}
	percentage := (float64(progress.BytesProcessed) / float64(progress.BytesTotal)) * 100
	processedGB := convertBytesToGB(progress.BytesProcessed)
	totalGB := convertBytesToGB(progress.BytesTotal)

	return fmt.Sprintf("pulling %.0f%% (%.0fGB/%.0fGB)", percentage, processedGB, totalGB)
}

func (s *GeranosPuller) trackProgress(progressChan chan dirimage.ProgressUpdate, cb func(st v1.ContainerStateWaiting)) {
	var lastProgressMessage string
	var lastPercentage float64

	for progressUpdate := range progressChan {
		percentage := (float64(progressUpdate.BytesProcessed) / float64(progressUpdate.BytesTotal)) * 100

		// Format the progress message
		progressMessage := formatProgress(progressUpdate)

		// Only log or update if there's a change in percentage or message
		if progressMessage != lastProgressMessage || int(percentage) != int(lastPercentage) {
			// Update log and callback with new progress
			log.Println(progressMessage)
			cb(v1.ContainerStateWaiting{
				Reason:  "Pulling",
				Message: progressMessage,
			})

			// Store the new progress message and percentage
			lastProgressMessage = progressMessage
			lastPercentage = percentage
		}
	}
}

// Pull TODO(tjarosik): Add secrets for image pulling
func (s *GeranosPuller) Pull(ctx context.Context, image string, pullPolicy v1.PullPolicy, cb func(st v1.ContainerStateWaiting)) (imageID string, err error) {
	switch pullPolicy {
	case v1.PullNever:
		return "", nil
	case v1.PullIfNotPresent:
		present, err := s.isPresent(image)
		if err != nil || present {
			return "", err
		}
		return s.doPull(ctx, image, cb)
	case v1.PullAlways:
		return s.doPull(ctx, image, cb)
	}
	return "", errors.New("invalid pull policy")
}

func (s *GeranosPuller) isPresent(image string) (present bool, err error) {
	ref, err := name.ParseReference(image, name.StrictValidation)
	if err != nil {
		return false, fmt.Errorf("failed to parse reference for image %s: %w", image, err)
	}
	return layout.NewMapper(s.imagesPath).ContainsAny(ref)
}

func (s *GeranosPuller) doPull(ctx context.Context, image string, cb func(st v1.ContainerStateWaiting)) (imageID string, err error) {
	ref, err := name.ParseReference(image, name.StrictValidation)
	if err != nil {
		return "", fmt.Errorf("failed to parse reference for image %s: %w", image, err)
	}

	progress := make(chan dirimage.ProgressUpdate)
	defer close(progress)
	go s.trackProgress(progress, cb)

	dirimageOptions := []dirimage.Option{
		dirimage.WithProgressChannel(progress),
		dirimage.WithLogFunction(func(fmt string, args ...any) {
		}),
	}
	lm := layout.NewMapper(s.imagesPath, dirimageOptions...)
	remoteOptions := []remote.Option{
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
	}
	img, err := remote.Image(ref, remoteOptions...)
	if err != nil {
		return "", fmt.Errorf("failed to pull image %s: %w", image, err)
	}
	imageDigest, err := img.Digest()
	if err != nil {
		return "", fmt.Errorf("failed to get image digest: %w", err)
	}
	err = lm.Write(ctx, img, ref)
	return imageDigest.String(), err
}
