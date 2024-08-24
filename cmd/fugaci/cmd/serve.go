package cmd

import (
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tomekjarosik/fugaci/pkg/bootstrap"
	"github.com/tomekjarosik/fugaci/pkg/fugaci"
	"github.com/tomekjarosik/fugaci/pkg/k8s"
	"github.com/virtual-kubelet/virtual-kubelet/node/nodeutil"
	"k8s.io/client-go/informers"
	"log"
	"sync"
	"time"
)

func NewCmdServe() *cobra.Command {
	var serveCommand = &cobra.Command{
		Use:   "serve",
		Short: "Start Fugaci provider for virtual-kubelet",
		Long:  `Start Fugaci provider for virtual-kubelet`,
		Args:  cobra.ExactArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			var cfg fugaci.Config
			if err := viper.Unmarshal(&cfg); err != nil {
				log.Fatalf("Unable to decode into struct, %v", err)
				return
			}

			client, err := k8s.NewClient(cfg.KubeConfigPath)
			if err != nil {
				log.Fatalf("Failed to create client: %v", err)
				return
			}

			// Create a shared informer factory with a default resync period
			informerFactory := informers.NewSharedInformerFactory(client, 30*time.Second)

			provider, err := fugaci.NewProvider(cfg)
			if err != nil {
				log.Fatalf("creating fugaci provider failed")
			}

			podController, err := bootstrap.NewPodController(cmd.Context(), informerFactory, client, provider)

			if err != nil {
				log.Fatalf("Failed to initialize Pod controller: %v", err)
				return
			}

			// Start the informers
			ctx := cmd.Context()
			informerFactory.Start(cmd.Context().Done())

			// Wait for informers to sync
			informerFactory.WaitForCacheSync(ctx.Done())

			go podController.Run(ctx, 1)
			select {
			case <-podController.Ready():
			case <-podController.Done():
			}
			if podController.Err() != nil {
				log.Fatalf("Error running pod controller: %v", err)
				return
			}
			log.Printf("Pod controller started succesfully")

			var wg sync.WaitGroup
			wg.Add(1)
			go bootstrap.StartAPIDaemon(ctx, provider, &wg, 10250)

			nodeutil.NewNode()
			nodeController, err := bootstrap.NewNodeController(cmd.Context(), client, cfg)
			err = nodeController.Run(ctx)
			if err != nil {
				log.Printf("Node controller finished with error %v", err)
			} else {
				log.Printf("Node controller finished gracefully")
			}
			wg.Wait()
			return
		},
	}

	return serveCommand
}
