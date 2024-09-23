package cmd

import (
	"crypto/tls"
	"flag"
	"fmt"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tomekjarosik/fugaci/pkg/fugaci"
	"github.com/tomekjarosik/fugaci/pkg/k8s"
	"github.com/virtual-kubelet/virtual-kubelet/log"
	logruslogger "github.com/virtual-kubelet/virtual-kubelet/log/logrus"
	"github.com/virtual-kubelet/virtual-kubelet/node"
	"github.com/virtual-kubelet/virtual-kubelet/node/nodeutil"
	"k8s.io/klog/v2"
	"net/http"
)

// See https://github.com/virtual-kubelet/azure-aci/blob/master/cmd/virtual-kubelet/main.go#L170

func NewCmdServe() *cobra.Command {
	viper.SetDefault("LogLevel", "info")
	var serveCommand = &cobra.Command{
		Use:   "serve",
		Short: "Start Fugaci provider for virtual-kubelet",
		Long:  `Start Fugaci provider for virtual-kubelet`,
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, args []string) {
			var cfg fugaci.Config
			if err := viper.Unmarshal(&cfg); err != nil {
				fmt.Printf("Unable to decode into struct, %v", err)
				return
			}
			if err := cfg.Validate(); err != nil {
				fmt.Printf("Invalid config, %v", err)
				return
			}
			fmt.Printf("Starting Fugaci provider with config: %+v\n", cfg)

			logger := logrus.StandardLogger()
			lvl, err := logrus.ParseLevel(cfg.LogLevel)
			if err != nil {
				logrus.WithError(err).Fatal("Error parsing log level")
			}
			logger.SetLevel(lvl)

			ctx := log.WithLogger(cmd.Context(), logruslogger.FromLogrus(logrus.NewEntry(logger)))

			k8sClient, err := k8s.NewClient(cfg.KubeConfigPath)
			if err != nil {
				log.G(ctx).Fatal(err)
				return
			}

			providerFunc := func(pConfig nodeutil.ProviderConfig) (nodeutil.Provider, node.NodeProvider, error) {
				p, err := fugaci.NewProvider(cmd.Context(), cfg)
				if err != nil {
					return nil, nil, err
				}
				p.ConfigureNode(cmd.Context(), pConfig.Node)
				return p, node.NewNaiveNodeProvider(), nil
			}
			configureRoutes := func(cfg *nodeutil.NodeConfig) error {
				mux := http.NewServeMux()
				cfg.Handler = mux
				return nodeutil.AttachProviderRoutes(mux)(cfg)
			}
			withNoClientCertFunc := func(config *tls.Config) error {
				config.ClientAuth = tls.NoClientCert
				return nil
			}
			vkNode, err := nodeutil.NewNode(cfg.NodeName,
				providerFunc,
				configureRoutes,
				nodeutil.WithClient(k8sClient),
				nodeutil.WithTLSConfig(nodeutil.WithKeyPairFromPath(cfg.TLS.CertPath, cfg.TLS.KeyPath), withNoClientCertFunc),
				func(c *nodeutil.NodeConfig) error {
					c.HTTPListenAddr = fmt.Sprintf(":%d", cfg.KubeletEndpointPort)
					return nil
				},
			)
			if err != nil {
				log.G(ctx).Fatal(err)
				return
			}
			err = vkNode.Run(cmd.Context())
			if err != nil {
				log.G(ctx).Fatal(err)
			}
			return
		},
	}

	flags := serveCommand.Flags()

	klogFlags := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(klogFlags)
	klogFlags.VisitAll(func(f *flag.Flag) {
		f.Name = "klog." + f.Name
		flags.AddGoFlag(f)
	})

	return serveCommand
}
