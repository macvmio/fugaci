package main

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"
)

// Helper function to handle errors and exit the program
func must(action string, err error) {
	if err != nil {
		log.Fatalf("Failed to %s: %v", action, err)
	}
}

type Provisioner struct {
	LocalInputs   LocalInputs
	LocalOutputs  LocalOutputs
	RemoteInputs  RemoteInputs
	RemoteOutputs RemoteOutputs
}

func NewProvisioner() (*Provisioner, error) {
	localInputs, err := NewLocalInputs()
	if err != nil {
		return nil, err
	}
	localOutputs, err := NewLocalOutputs(*localInputs)
	if err != nil {
		return nil, err
	}
	remoteInputs, err := NewRemoteInputs()
	if err != nil {
		return nil, err
	}
	remoteOutputs, err := NewRemoteOutputs(*remoteInputs)
	if err != nil {
		return nil, err
	}

	return &Provisioner{
		LocalInputs:   *localInputs,
		LocalOutputs:  *localOutputs,
		RemoteInputs:  *remoteInputs,
		RemoteOutputs: *remoteOutputs,
	}, nil
}

func (p *Provisioner) Provision() {
	fmt.Println("Starting environment setup...")
	must("start docker compose", p.startDockerCompose())
	must("process kubeconfig.yaml file", p.processKubeconfigFile())
	must("generate TLS Certs for Mac Workstation", p.generateNodeTLSCerts())
	must("extract Certificate Authority from kubeconfig.yaml", p.extractCertificateAuthorityData())
	must("validate TLS certs are signed with CA", p.validateCertKeyPair())
	must("create Fugaci username/password secret in Kubernetes", p.createFugaciSSHSecret())
	must("generate Fugaci node configuration content", p.generateFugaciConfigContent())
	must("deploy files to Mac Workstation", p.deployFilesToMacWorkstation())
	fmt.Println("Environment setup completed successfully.")

	fmt.Println("To use kubectl with this configuration, set the following environment variable:")
	fmt.Printf("export KUBECONFIG=%s\n", p.LocalOutputs.KubeconfigDestinationPath)
}

func (p *Provisioner) startDockerCompose() error {
	fmt.Println("Starting Docker Compose services...")

	cmd := exec.Command("docker", "compose", "up", "-d")
	cmd.Dir = p.LocalInputs.DockerDirectoryPath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("frror starting Docker Compose: %v", err)
	}
	fmt.Println("Docker Compose services started.")
	return nil
}

func (p *Provisioner) processKubeconfigFile() error {
	fmt.Println("Waiting for kubeconfig.yaml to be created...")

	// Wait for the kubeconfig.yaml file to be created
	timeout := time.After(60 * time.Second) // Adjust timeout as needed
	ticker := time.Tick(1 * time.Second)

	for {
		select {
		case <-timeout:
			fmt.Println("Timed out waiting for kubeconfig.yaml to be created")
			return fmt.Errorf("timed out waiting for kubeconfig.yaml to be created")
		case <-ticker:
			if _, err := os.Stat(p.LocalInputs.KubeconfigSourcePath); err == nil {
				fmt.Println("kubeconfig.yaml has been created.")
				goto ProcessFile
			}
		}
	}

ProcessFile:
	// Read the kubeconfig file
	contentBytes, err := os.ReadFile(p.LocalInputs.KubeconfigSourcePath)
	if err != nil {
		return fmt.Errorf("Failed to read %s: %v\n", p.LocalInputs.KubeconfigSourcePath, err)
	}
	content := string(contentBytes)

	// Replace the server IP
	oldServerLine := "server: https://127.0.0.1:6443"
	newServerLine := fmt.Sprintf("server: https://%s:16443", p.LocalInputs.ServerIPAddress)
	modifiedContent := strings.Replace(content, oldServerLine, newServerLine, -1)

	// Ensure the destination directory exists
	err = os.MkdirAll(filepath.Dir(p.LocalOutputs.KubeconfigDestinationPath), 0755)
	if err != nil {
		return fmt.Errorf("failed to create directory %s: %v\n", filepath.Dir(p.LocalOutputs.KubeconfigDestinationPath), err)
	}

	// Write the modified content to the destination file
	err = os.WriteFile(p.LocalOutputs.KubeconfigDestinationPath, []byte(modifiedContent), 0644)
	if err != nil {
		return fmt.Errorf("failed to write %s: %v\n", p.LocalOutputs.KubeconfigDestinationPath, err)
	}

	err = p.waitForKubernetes(10*time.Second, p.LocalOutputs.KubeconfigDestinationPath)
	if err != nil {
		return fmt.Errorf("failed to wait for Kubernetes API to become ready: %v", err)
	}
	fmt.Printf("Modified kubeconfig saved to %s\n", p.LocalOutputs.KubeconfigDestinationPath)
	return nil
}

func (p *Provisioner) generateFugaciConfigContent() error {
	fmt.Println("Generating Fugaci configuration content...")
	configData := struct {
		NodeName            string
		KubeConfigPath      string
		CurieVirtualization struct {
			BinaryPath   string
			DataRootPath string
		}
		InternalIP string
		TLS        struct {
			KeyPath                  string
			CertPath                 string
			CertificateAuthorityPath string
		}
	}{
		NodeName:       p.LocalInputs.MacNodeName,
		KubeConfigPath: p.RemoteOutputs.KubeconfigPath,
		InternalIP:     p.RemoteInputs.MacNodeIPAddress,
		CurieVirtualization: struct {
			BinaryPath   string
			DataRootPath string
		}{
			BinaryPath:   p.RemoteInputs.CurieBinaryPath,
			DataRootPath: p.RemoteInputs.CurieDataRootPath,
		},
		TLS: struct {
			KeyPath                  string
			CertPath                 string
			CertificateAuthorityPath string
		}{
			KeyPath:                  p.RemoteOutputs.TLSKeyPath,
			CertPath:                 p.RemoteOutputs.TLSCertPath,
			CertificateAuthorityPath: p.RemoteOutputs.CertificateAuthorityPath,
		},
	}

	// Parse the template
	tmpl, err := template.New("config").Parse(configTemplate)
	if err != nil {
		return fmt.Errorf("failed to parse configuration template: %v", err)
	}

	// Execute the template and store the result in a string
	var configContent strings.Builder
	err = tmpl.Execute(&configContent, configData)
	if err != nil {
		return fmt.Errorf("failed to execute configuration template: %v", err)
	}
	err = os.WriteFile(p.LocalOutputs.MacNodeFugaciConfigPath, []byte(configContent.String()), 0644)
	if err != nil {
		return fmt.Errorf("failed to write configuration template: %v", err)
	}

	fmt.Println("Configuration content generated successfully.")
	return nil
}

// deployFileToRemote copies a local file to a remote machine using scp.
func (p *Provisioner) deployFileToRemote(localPath, remoteHost, remotePath string) error {
	cmd := exec.Command("scp", localPath, fmt.Sprintf("%s:%s", remoteHost, remotePath))

	// Run the command and capture the output
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to execute scp command: %v\nOutput: %s", err, string(output))
	}

	fmt.Printf("Successfully deployed '%s' to '%s:%s'\n", localPath, remoteHost, remotePath)
	return nil
}

// createRemoteDirectory ensures that the specified directory exists on the remote machine.
func (p *Provisioner) createRemoteDirectory(remoteHost, remoteDir string) error {
	cmd := exec.Command("ssh", remoteHost, fmt.Sprintf("mkdir -p '%s'", remoteDir))
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to create remote directory: %v\nOutput: %s", err, string(output))
	}
	return nil
}

func (p *Provisioner) deployFilesToMacWorkstation() error {
	if err := p.createRemoteDirectory(p.RemoteInputs.SSHNodeName, p.RemoteOutputs.DevEnvDirPath); err != nil {
		return fmt.Errorf("failed to create remote directory: %v", err)
	}

	filesToDeploy := map[string]string{
		p.LocalOutputs.TLSKeyPath:                p.RemoteOutputs.TLSKeyPath,
		p.LocalOutputs.TLSCertPath:               p.RemoteOutputs.TLSCertPath,
		p.LocalOutputs.CertificateAuthorityPath:  p.RemoteOutputs.CertificateAuthorityPath,
		p.LocalOutputs.KubeconfigDestinationPath: p.RemoteOutputs.KubeconfigPath,
		p.LocalOutputs.MacNodeFugaciConfigPath:   p.RemoteOutputs.FugaciConfigPath,
	}
	for localPath, remotePath := range filesToDeploy {
		if err := p.deployFileToRemote(localPath, p.RemoteInputs.SSHNodeName, remotePath); err != nil {
			return fmt.Errorf("failed to deploy file '%s': %v", filepath.Base(remotePath), err)
		}
		if strings.Contains(localPath, "kubeconfig") {
			continue
		}
		os.Remove(localPath)
	}
	fmt.Println("All files deployed successfully.")
	return nil
}

func (p *Provisioner) waitForKubernetes(timeout time.Duration, kubeconfig string) error {
	start := time.Now()

	// Retry logic with a timeout
	for time.Since(start) < timeout {
		if err := checkKubectlConnection(kubeconfig); err != nil {
			fmt.Printf("Kubernetes API not ready: %v. Retrying...\n", err)
			time.Sleep(5 * time.Second)
			continue
		}

		// Kubernetes is ready
		fmt.Println("Kubernetes is initialized and ready!")
		return nil
	}

	return fmt.Errorf("timeout reached: Kubernetes did not become ready within %v", timeout)
}

// checkKubectlConnection verifies if `kubectl` can reach the API server.
func checkKubectlConnection(kubeconfig string) error {
	cmd := exec.Command("kubectl", "get", "namespaces", "--kubeconfig", kubeconfig)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to connect to Kubernetes API: %s", stderr.String())
	}
	return nil
}

func (p *Provisioner) createFugaciSSHSecret() error {
	fmt.Println("Creating Kubernetes secret 'fugaci-ssh-secret'...")
	// Check if the kubeconfig file exists
	if _, err := os.Stat(p.LocalOutputs.KubeconfigDestinationPath); os.IsNotExist(err) {
		fmt.Printf("Kubeconfig file not found at %s\n", p.LocalOutputs.KubeconfigDestinationPath)
		return err
	}

	// Prepare the kubectl command
	cmd := exec.Command("kubectl", "create", "secret", "generic", "fugaci-ssh-secret",
		// TODO: Extract to constants
		fmt.Sprintf("--from-literal=%s=%s", FugaciSSHUsernameEnvVariable, TestMacOSImageSSHUsername),
		fmt.Sprintf("--from-literal=%s=%s", FugaciSSHPasswordEnvVariable, TestMacOSImageSSHPassword),
		"--kubeconfig", p.LocalOutputs.KubeconfigDestinationPath)

	// Capture the output and error
	output, err := cmd.CombinedOutput()
	if err == nil {
		fmt.Println("Secret 'fugaci-ssh-secret' created successfully.")
		return nil
	}
	if strings.Contains(string(output), "already exists") {
		fmt.Println("skipping: secret 'fugaci-ssh-secret' already exists.")
		return nil
	}
	fmt.Printf("Failed to create secret: %v\nOutput: %s\n", err, string(output))
	return err
}

// validateCertKeyPair validates the presence, matching, and signing of a certificate and key pair.
func (p *Provisioner) validateCertKeyPair() error {
	keyFile := p.LocalOutputs.TLSKeyPath
	crtFile := p.LocalOutputs.TLSCertPath

	// Check if certificate and key files exist
	if _, err := os.Stat(crtFile); os.IsNotExist(err) {
		return fmt.Errorf("certificate file %s not found", crtFile)
	}
	if _, err := os.Stat(keyFile); os.IsNotExist(err) {
		return fmt.Errorf("key file %s not found", keyFile)
	}

	// Load and parse the certificate
	crtData, err := os.ReadFile(crtFile)
	if err != nil {
		return fmt.Errorf("failed to read certificate file: %v", err)
	}
	crtBlock, _ := pem.Decode(crtData)
	if crtBlock == nil || crtBlock.Type != "CERTIFICATE" {
		return fmt.Errorf("invalid certificate file format: %s", crtFile)
	}
	cert, err := x509.ParseCertificate(crtBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse certificate: %v", err)
	}

	// Load and parse the private key
	keyData, err := os.ReadFile(keyFile)
	if err != nil {
		return fmt.Errorf("failed to read key file: %v", err)
	}
	keyBlock, _ := pem.Decode(keyData)
	if keyBlock == nil {
		return fmt.Errorf("invalid key file format: %s", keyFile)
	}

	// Parse the private key and check the type
	var privateKey interface{}
	switch keyBlock.Type {
	case "PRIVATE KEY", "RSA PRIVATE KEY":
		privateKey, err = x509.ParsePKCS1PrivateKey(keyBlock.Bytes)
	case "EC PRIVATE KEY":
		privateKey, err = x509.ParseECPrivateKey(keyBlock.Bytes)
	default:
		return fmt.Errorf("unsupported key type: %s in file %s", keyBlock.Type, keyFile)
	}
	if err != nil {
		return fmt.Errorf("failed to parse private key: %v", err)
	}

	// Verify that the private key matches the certificate's public key
	switch pub := cert.PublicKey.(type) {
	case *rsa.PublicKey:
		priv, ok := privateKey.(*rsa.PrivateKey)
		if !ok || pub.N.Cmp(priv.N) != 0 {
			return fmt.Errorf("certificate and key do not match")
		}
	case *ecdsa.PublicKey:
		priv, ok := privateKey.(*ecdsa.PrivateKey)
		if !ok || pub.X.Cmp(priv.X) != 0 || pub.Y.Cmp(priv.Y) != 0 {
			return fmt.Errorf("certificate and key do not match")
		}
	default:
		return fmt.Errorf("unsupported public key type in certificate")
	}

	// Validate certificate against CA
	caFile := p.LocalOutputs.CertificateAuthorityPath
	caData, err := os.ReadFile(caFile)
	if err != nil {
		return fmt.Errorf("failed to read CA file: %v", err)
	}
	caBlock, _ := pem.Decode(caData)
	if caBlock == nil || caBlock.Type != "CERTIFICATE" {
		return fmt.Errorf("invalid CA file format: %s", caFile)
	}
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse CA certificate: %v", err)
	}

	roots := x509.NewCertPool()
	roots.AddCert(caCert)
	opts := x509.VerifyOptions{
		Roots: roots,
	}
	if _, err := cert.Verify(opts); err != nil {
		return fmt.Errorf("certificate is not signed by the specified CA: %v", err)
	}

	fmt.Printf("Validation successful: %s and %s are a matching pair, signed by %s\n", crtFile, keyFile, caFile)
	return nil
}

func (p *Provisioner) generateNodeTLSCerts() error {
	generateNodeTLScriptPathPath := path.Join(p.LocalInputs.Cwd, "../generate-node-tls-certs.sh")

	// Prepare the command
	cmd := exec.Command(generateNodeTLScriptPathPath, p.LocalInputs.MacNodeName, p.LocalInputs.MacNodeIPAddress)
	cmd.Env = append(os.Environ(), fmt.Sprintf("KUBECONFIG=%s", p.LocalOutputs.KubeconfigDestinationPath))
	// Run the command and capture the output
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to execute %s: %w\nOutput: %s", cmd, err, string(output))
	}

	fmt.Printf("Successfully executed '%s'\n", cmd)
	return nil
}

func (p *Provisioner) extractCertificateAuthorityData() error {
	content, err := os.ReadFile(p.LocalOutputs.KubeconfigDestinationPath)
	if err != nil {
		return fmt.Errorf("failed to read kubeconfig file: %v", err)
	}

	// Use a regular expression to extract certificate-authority-data
	re := regexp.MustCompile(`certificate-authority-data:\s*([A-Za-z0-9+/=]+)`)
	matches := re.FindStringSubmatch(string(content))
	if len(matches) < 2 {
		return fmt.Errorf("certificate-authority-data not found in kubeconfig")
	}

	// Decode the base64-encoded certificate data
	decodedData, err := base64.StdEncoding.DecodeString(matches[1])
	if err != nil {
		return fmt.Errorf("failed to decode certificate-authority-data: %v", err)
	}

	// Save the decoded certificate data as a .pem file
	err = os.WriteFile(p.LocalOutputs.CertificateAuthorityPath, decodedData, 0644)
	if err != nil {
		return fmt.Errorf("failed to write .pem file: %v", err)
	}

	fmt.Printf("Certificate saved to %s\n", p.LocalOutputs.CertificateAuthorityPath)
	return nil
}
