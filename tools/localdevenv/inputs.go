package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const MacWorkstationNodeName = "mac-workstation"
const K3STokenEnvVariable = "K3S_TOKEN"
const FugaciMacWorkstationIPAddressEnvVariable = "FUGACI_MAC_WORKSTATION_IP_ADDRESS"
const FugaciK3SServerIPAddressEnvVariable = "FUGACI_K3S_SERVER_IP_ADDRESS"

const FugaciSSHUsernameEnvVariable = "FUGACI_SSH_USERNAME"
const FugaciSSHPasswordEnvVariable = "FUGACI_SSH_PASSWORD"
const TestMacOSImageSSHUsername = "agent"
const TestMacOSImageSSHPassword = "password"

const configTemplate = `nodeName: {{.NodeName}}
kubeConfigPath: {{.KubeConfigPath}}

curieVirtualization:
  binaryPath: {{.CurieVirtualization.BinaryPath}}
  dataRootPath: {{.CurieVirtualization.DataRootPath}}

internalIP: "{{.InternalIP}}"

TLS:
  keyPath: {{.TLS.KeyPath}}
  certPath: {{.TLS.CertPath}}
  certificateAuthorityPath: {{.TLS.CertificateAuthorityPath}}
`

type LocalInputs struct {
	Cwd                  string
	DockerDirectoryPath  string
	KubeconfigSourcePath string
	ServerIPAddress      string

	MacNodeName      string
	MacNodeIPAddress string

	K3SAgentContainer struct {
		Name         string
		ClientCAPath string
	}
}

type RemoteInputs struct {
	SSHNodeName      string
	MacNodeIPAddress string
	Username         string
	HomeDir          string
	FugaciDirPath    string

	CurieBinaryPath   string
	CurieDataRootPath string
}

func NewLocalInputs() (*LocalInputs, error) {
	_, scriptPath, _, ok := runtime.Caller(0)
	if !ok {
		return nil, fmt.Errorf("could not determine local input script path")
	}
	scriptDir := filepath.Dir(scriptPath)
	return &LocalInputs{
		Cwd:                  scriptDir,
		DockerDirectoryPath:  filepath.Join(scriptDir, "..", "docker"),
		KubeconfigSourcePath: filepath.Clean(filepath.Join(scriptDir, "..", "docker", "kubeconfig.yaml")),
		ServerIPAddress:      os.Getenv(FugaciK3SServerIPAddressEnvVariable),
		MacNodeName:          MacWorkstationNodeName,
		MacNodeIPAddress:     os.Getenv(FugaciMacWorkstationIPAddressEnvVariable),
		K3SAgentContainer: struct {
			Name         string
			ClientCAPath string
		}{
			Name:         "agent",
			ClientCAPath: "/var/lib/rancher/k3s/agent/client-ca.crt",
		},
	}, nil
}

func NewRemoteInputs() (*RemoteInputs, error) {
	// Command to get the username and home directory
	cmd := exec.Command("ssh", MacWorkstationNodeName, "echo $(whoami):$HOME")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to execute SSH command: %v", err)
	}

	result := strings.TrimSpace(string(output))
	parts := strings.Split(result, ":")
	if len(parts) != 2 {
		return nil, fmt.Errorf("unexpected output from SSH command")
	}

	username := parts[0]
	homeDir := parts[1]

	cmd = exec.Command("ssh", MacWorkstationNodeName, "PATH=/usr/local/bin:$PATH command -v curie")
	output, err = cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to find 'curie' binary on '%s': %v", MacWorkstationNodeName, err)
	}

	curiePath := strings.TrimSpace(string(output))
	if curiePath == "" {
		return nil, fmt.Errorf("'curie' binary not found on '%s'", MacWorkstationNodeName)

	}
	return &RemoteInputs{
		SSHNodeName:       MacWorkstationNodeName,
		MacNodeIPAddress:  os.Getenv(FugaciMacWorkstationIPAddressEnvVariable),
		Username:          username,
		HomeDir:           homeDir,
		FugaciDirPath:     filepath.Join(homeDir, ".fugaci"),
		CurieBinaryPath:   curiePath,
		CurieDataRootPath: filepath.Join(homeDir, ".curie"),
	}, nil
}
