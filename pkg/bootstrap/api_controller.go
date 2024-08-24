package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"github.com/tomekjarosik/fugaci/pkg/fugaci"
	"github.com/virtual-kubelet/virtual-kubelet/node/api"
	"net/http"
	"sync"
	"time"
)

func StartAPIDaemon(ctx context.Context, p *fugaci.Provider, wg *sync.WaitGroup, port int) {
	defer wg.Done()

	mux := http.NewServeMux()
	podHandlerCfg := api.PodHandlerConfig{
		RunInContainer:        p.ContainerExecHandler,
		AttachToContainer:     p.ContainerAttachHandler,
		PortForward:           p.PortForwardHandler,
		GetContainerLogs:      p.ContainerLogsHandler,
		GetPods:               p.GetPods,
		GetPodsFromKubernetes: nil,
		GetStatsSummary:       nil,
		GetMetricsResource:    nil,
		StreamIdleTimeout:     0,
		StreamCreationTimeout: 0,
	}
	api.AttachPodRoutes(podHandlerCfg, mux, false)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		// Shutdown the server gracefully
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			fmt.Println("HTTP server shutdown error:", err)
		}
	}()

	fmt.Println("HTTP server is listening on port 8080...")
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		fmt.Println("HTTP server error:", err)
	}

	fmt.Println("HTTP server has stopped")
}
