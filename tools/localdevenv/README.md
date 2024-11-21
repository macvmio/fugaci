
# Local Development Environment Setup

This script sets up a local development environment for testing and provisioning resources.

## Prerequisites

Ensure the following are installed and properly configured:

- **Environment Variables**:
  - **K3S_TOKEN**: Token for K3S.
  - **FUGACI_MAC_WORKSTATION_IP_ADDRESS**: IP address of the Mac workstation reachable from K3S server.
  - **FUGACI_K3S_SERVER_IP_ADDRESS**: IP address of the K3S server reachable from Mac workstation.

- **Executables**:
  - kubectl
  - cfssl
  - cfssljson

- **SSH Connectivity**:
  - SSH access to the machine named **mac-workstation**.
  - The binary 'curie' must be present on **mac-workstation**.

## Usage

Run the script using the following command:
```sh
	go run tools/localdevenv
```
## Description

1. **Verification**:
   - Checks required environment variables are set.
   - Verifies required executables are installed.
   - Validates SSH connectivity and necessary binaries on the remote machine.

2. **Provisioning**:
   - Provisions the required resources if all checks pass.
