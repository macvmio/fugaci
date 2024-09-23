package fugaci

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type Config struct {
	NodeName            string `yaml:"nodeName"`
	KubeConfigPath      string `yaml:"kubeConfigPath"`
	LogLevel            string `yaml:"logLevel"`
	CurieBinaryPath     string `yaml:"curieBinaryPath"`
	CurieDataRootPath   string `yaml:"curieDataRootPath"`
	KubeletEndpointPort int32  `yaml:"kubeletEndpointPort"`

	TLS struct {
		KeyPath  string `yaml:"keyPath"`
		CertPath string `yaml:"certPath"`
	} `yaml:"tls"`
}

func (c *Config) Validate() error {
	// Map to store validation errors for specific paths
	pathErrors := make(map[string]string)

	// Validate the paths
	paths := map[string]string{
		"KubeConfigPath":    c.KubeConfigPath,
		"CurieBinaryPath":   c.CurieBinaryPath,
		"CurieDataRootPath": c.CurieDataRootPath,
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
