package fugaci

import (
	"context"
	"fmt"
	regv1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/mobileinf/geranos/pkg/transporter"
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
func formatProgress(progress transporter.ProgressUpdate) string {
	if progress.BytesTotal == 0 {
		return "pulling 0% (0GB/0GB)" // Handle case where total is 0 to avoid division by zero
	}
	percentage := (float64(progress.BytesProcessed) / float64(progress.BytesTotal)) * 100
	processedGB := convertBytesToGB(progress.BytesProcessed)
	totalGB := convertBytesToGB(progress.BytesTotal)

	return fmt.Sprintf("pulling %.0f%% (%.0fGB/%.0fGB)", percentage, processedGB, totalGB)
}

func (s *GeranosPuller) trackProgress(progressChan chan transporter.ProgressUpdate, cb func(st v1.ContainerStateWaiting)) {
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
func (s *GeranosPuller) Pull(ctx context.Context, image string, pullPolicy v1.PullPolicy, cb func(st v1.ContainerStateWaiting)) (regv1.Hash, *regv1.Manifest, error) {
	opts := []transporter.Option{
		transporter.WithContext(ctx),
	}
	if pullPolicy != v1.PullNever {
		if pullPolicy == v1.PullAlways {
			opts = append(opts, transporter.WithForce(true))
		}
		progress := make(chan transporter.ProgressUpdate)
		defer close(progress)
		go s.trackProgress(progress, cb)
		opts = append(opts, transporter.WithProgressChannel(progress))

		err := transporter.Pull(image, opts...)
		if err != nil {
			return regv1.Hash{}, nil, fmt.Errorf("pull image: %w", err)
		}
	}
	manifest, err := transporter.ReadManifest(image)
	if err != nil {
		return regv1.Hash{}, nil, fmt.Errorf("read image manigest: %w", err)
	}
	digest, err := transporter.ReadDigest(image, opts...)
	if err != nil {
		return regv1.Hash{}, nil, fmt.Errorf("read image digest: %w", err)
	}
	return digest, manifest, err
}
