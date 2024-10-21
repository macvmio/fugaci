package fugaci

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	NodeName            string `mapstructure:"nodeName"`
	KubeConfigPath      string `mapstructure:"kubeConfigPath"`
	LogLevel            string `mapstructure:"logLevel"`
	CurieVirtualization struct {
		BinaryPath   string `mapstructure:"binaryPath"`
		DataRootPath string `mapstructure:"dataRootPath"`
	} `mapstructure:"curieVirtualization"`

	KubeletEndpointPort int32 `mapstructure:"kubeletEndpointPort"`
	// Needs to be reachable by Kubernetes control plane
	internalIP string `mapstructure:"internalIP"`

	TLS struct {
		KeyPath  string `mapstructure:"keyPath"`
		CertPath string `mapstructure:"certPath"`
		// This is CA you can obtain from decoded .kube/config's certificate-authority-data
		// TODO(tjarosik): automatically extract that
		CertificateAuthorityPath string `mapstructure:"certificateAuthorityPath"`
	} `mapstructure:"tls"`
}

func (c *Config) Validate() error {
	// Map to store validation errors for specific paths
	pathErrors := make(map[string]string)

	// Validate the paths
	paths := map[string]string{
		"KubeConfigPath":    c.KubeConfigPath,
		"CurieBinaryPath":   c.CurieVirtualization.BinaryPath,
		"CurieDataRootPath": c.CurieVirtualization.DataRootPath,
		"TLS.KeyPath":       c.TLS.KeyPath,
		"TLS.CertPath":      c.TLS.CertPath,
	}

	for name, path := range paths {
		if path == "" {
			pathErrors[name] = "path is empty"
		} else if !filepath.IsAbs(path) {
			pathErrors[name] = "path is not absolute"
		} else if _, err := os.Stat(path); os.IsNotExist(err) {
			pathErrors[name] = "path does not exist"
		}
	}

	// If there are any path validation errors, return them
	if len(pathErrors) > 0 {
		var errMsg string
		for name, err := range pathErrors {
			errMsg += fmt.Sprintf("%s: %s\n", name, err)
		}
		return errors.New(errMsg)
	}

	// Default KubeletEndpointPort to 10250 if it's 0
	if c.KubeletEndpointPort == 0 {
		c.KubeletEndpointPort = 10250
	}

	return nil
}
