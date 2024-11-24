package main

import (
	"fmt"
	"log"
	"os"
	"text/template"
)

func generateReadme() {
	const readmeTemplate = `
# Local Development Environment Setup for Fugaci

This script helps you set up a local development environment for **Fugaci**, a macOS virtual kubelet connected to a K3S server. 

## Requirements

To run Fugaci, you need the following setup:

1. **Machines**:
   - A **Linux machine** for running the K3S server.
   - A **macOS machine** configured as the Fugaci kubelet and connected to the K3S server.

   If you only have a macOS machine:
   - You can simulate a Linux environment using a virtual machine (VM).
   - Running both K3S and Fugaci on a single macOS machine is possible but not officially tested.

2. **Environment Variables**:
   Set the following environment variables before running the script:
   - **{{.K3STokenEnvVariable}}**: Token for K3S authentication. It can be any unique value as long as it remains consistent.
   - **{{.FugaciMacWorkstationIPAddressEnvVariable}}**: IP address of the macOS workstation, reachable from the K3S server.
   - **{{.FugaciK3SServerIPAddressEnvVariable}}**: IP address of the K3S server, reachable from the macOS workstation.

3. **Required Tools**:
   Ensure the following tools are installed and accessible in your PATH:
   - ` + "`kubectl`" + ` (for Kubernetes CLI operations)
   - ` + "`cfssl`" + ` and ` + "`cfssljson`" + ` (for certificate management)

4. **SSH Access**:
   - Ensure SSH connectivity to the macOS machine (` + "`{{.MacWorkstationNodeName}}`" + `).
   - The **'curie' binary** must be present and executable on the macOS machine.

## How to Use

1. **Run the Script**:
Run the script using the following command:
` + "```sh" + `
	go run tools/localdevenv
` + "```" + `

2. Start Fugaci on MacOS node with config from ~/.fugaci/devenv-k3s/fugaci-config.yaml

3. **Test the Setup**:
   After the setup, you can run the following commands to validate functionality:
   - Deploy a sample pod:
` + "```sh" + `
	kubectl create -f pkg/fugaci_test/testdata/pod1-basic-running.yaml
` + "```" + `
   - View logs for the test pod:
` + "```sh" + `
	kubectl logs testpod1
` + "```" + `
   - Open an interactive shell in the test pod:
` + "```sh" + `
	kubectl exec --stdin --tty testpod1 -- /bin/bash
` + "```" + `
   - Port-forward from the pod to your local machine:
` + "```sh" + `
	kubectl port-forward testpod1 5900:5900
` + "```" + `

## What the Script Does

### 1. Verification
The script ensures the following prerequisites are met:
- All required environment variables are set.
- Necessary executables are installed on both machines.
- SSH connectivity is configured, and the ` + "`curie`" + ` binary is available on the macOS machine.

### 2. Provisioning
If verification passes, the script provisions the following resources:
- Sets up the K3S server.
- Configures the macOS workstation as a kubelet.
- Establishes communication between the two machines.

---

**Note**: This setup is intended for local development and testing purposes only. For production deployments, additional configurations and security measures may be required.

`

	// Struct for template data
	data := struct {
		K3STokenEnvVariable                      string
		FugaciMacWorkstationIPAddressEnvVariable string
		FugaciK3SServerIPAddressEnvVariable      string
		MacWorkstationNodeName                   string
	}{
		K3STokenEnvVariable:                      K3STokenEnvVariable,
		FugaciMacWorkstationIPAddressEnvVariable: FugaciMacWorkstationIPAddressEnvVariable,
		FugaciK3SServerIPAddressEnvVariable:      FugaciK3SServerIPAddressEnvVariable,
		MacWorkstationNodeName:                   MacWorkstationNodeName,
	}

	// Create the README template
	tmpl, err := template.New("readme").Parse(readmeTemplate)
	if err != nil {
		log.Fatalf("Failed to parse README template: %v", err)
	}

	// Write the template to a README.md file
	file, err := os.Create("README.md")
	if err != nil {
		log.Fatalf("Failed to create README.md file: %v", err)
	}
	defer file.Close()

	// Execute the template with data
	err = tmpl.Execute(file, data)
	if err != nil {
		log.Fatalf("Failed to execute template: %v", err)
	}

	fmt.Println("README.md file generated successfully.")
}
