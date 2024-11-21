package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
)

type LocalOutputs struct {
	KubeconfigDestinationPath string
	TLSKeyPath                string
	TLSCertPath               string
	CertificateAuthorityPath  string
	MacNodeFugaciConfigPath   string
}

type RemoteOutputs struct {
	DevEnvDirPath            string
	TLSKeyPath               string
	TLSCertPath              string
	CertificateAuthorityPath string
	KubeconfigPath           string
	FugaciConfigPath         string
}

func NewLocalOutputs(inputs LocalInputs) (*LocalOutputs, error) {
	return &LocalOutputs{
		KubeconfigDestinationPath: path.Join(os.Getenv("HOME"), ".kube", "devenv", "kubeconfig.yaml"),
		TLSKeyPath:                filepath.Clean(filepath.Join(inputs.Cwd, "..", inputs.MacNodeName+"-key.pem")),
		TLSCertPath:               filepath.Clean(filepath.Join(inputs.Cwd, "..", inputs.MacNodeName+"-crt.pem")),
		CertificateAuthorityPath:  filepath.Clean(filepath.Join(inputs.Cwd, "..", "k3s-CA.pem")),
		MacNodeFugaciConfigPath:   path.Join(os.Getenv("HOME"), ".kube", "devenv", "fugaci-config.yaml"),
	}, nil
}

func NewRemoteOutputs(inputs RemoteInputs) (*RemoteOutputs, error) {
	devEnvPath := filepath.Join(inputs.FugaciDirPath, "devenv-k3s")
	return &RemoteOutputs{
		DevEnvDirPath:            devEnvPath,
		TLSKeyPath:               filepath.Join(devEnvPath, fmt.Sprintf("%s-key.pem", inputs.SSHNodeName)),
		TLSCertPath:              filepath.Join(devEnvPath, fmt.Sprintf("%s-crt.pem", inputs.SSHNodeName)),
		CertificateAuthorityPath: filepath.Join(devEnvPath, "k3s-CA.pem"),
		KubeconfigPath:           filepath.Join(devEnvPath, "k3s.yaml"),
		FugaciConfigPath:         filepath.Join(devEnvPath, "fugaci-config.yaml"),
	}, nil
}
