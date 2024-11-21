package main

import (
	"fmt"
	"log"
	"os"
	"text/template"
)

func generateReadme() {
	const readmeTemplate = `
# Local Development Environment Setup

This script sets up a local development environment for testing and provisioning resources.

## Prerequisites

Ensure the following are installed and properly configured:

- **Environment Variables**:
  - **{{.K3STokenEnvVariable}}**: Token for K3S.
  - **{{.FugaciMacWorkstationIPAddressEnvVariable}}**: IP address of the Mac workstation reachable from K3S server.
  - **{{.FugaciK3SServerIPAddressEnvVariable}}**: IP address of the K3S server reachable from Mac workstation.

- **Executables**:
  - kubectl
  - cfssl
  - cfssljson

- **SSH Connectivity**:
  - SSH access to the machine named **{{.MacWorkstationNodeName}}**.
  - The binary 'curie' must be present on **{{.MacWorkstationNodeName}}**.

## Usage

Run the script using the following command:
` + "```sh" + `
	go run tools/localdevenv
` + "```" + `
## Description

1. **Verification**:
   - Checks required environment variables are set.
   - Verifies required executables are installed.
   - Validates SSH connectivity and necessary binaries on the remote machine.

2. **Provisioning**:
   - Provisions the required resources if all checks pass.
`
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

	tmpl, err := template.New("readme").Parse(readmeTemplate)
	if err != nil {
		log.Fatalf("Failed to parse README template: %v", err)
	}

	file, err := os.Create("README.md")
	if err != nil {
		log.Fatalf("Failed to create README.md file: %v", err)
	}
	defer file.Close()

	err = tmpl.Execute(file, data)
	if err != nil {
		log.Fatalf("Failed to execute template: %v", err)
	}

	fmt.Println("README.md file generated successfully.")
}
