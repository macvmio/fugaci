![Fugaci](resources/fugaci-logo.png)

Fugaci is a Kubernetes-based system for serving ephemeral macOS virtual machines (VMs). It integrates seamlessly with Kubernetes, allowing macOS-specific workflows to be managed through standard Kubernetes APIs (`kubectl`). Fugaci presents itself as a Kubernetes node with specific taints, enabling it to run macOS workloads while not interfering with standard Linux container workflows.

[![Watch the video](https://img.youtube.com/vi/DbzaP82zl7c/maxresdefault.jpg)](https://www.youtube.com/watch?v=DbzaP82zl7c)

## A Building Block for macOS workflows

Fugaci is designed as a foundational building block rather than a complete out-of-the-box solution.
Just as Kubernetes provides a platform for orchestrating containers upon which complex systems are built,
Fugaci offers the missing piece for managing ephemeral macOS virtual machines within Kubernetes. 
This allows developers and system architects to build customized workflows and higher-level solutions
tailored to their specific macOS deployment needs.


## Status

**Experimental**: Fugaci is currently experimental and not recommended for production use. However, it has been successfully used to run thousands of VMs over multiple days with continuous integration systems.

## Features

- **Ephemeral macOS VMs**: Run macOS VMs that are created and destroyed as needed, optimizing resource utilization.
- **Kubernetes Integration**: Manage macOS VMs using standard Kubernetes commands and APIs.
- **Taint-Based Scheduling**: Use Kubernetes taints to control workload placement on macOS nodes.
- **Single Binary Distribution**: Easy deployment with a single Go binary.

## Getting Started

### Prerequisites

- **macOS Host**: Fugaci runs as a daemon on macOS and assumes it is the only virtualization software running on the node.
- **Curie Binary**: Fugaci depends on the "curie" binary to be present on the system.
- **Kubernetes Cluster**: An existing Kubernetes cluster with access to the kubeconfig file.
- **TLS Certificates**: TLS key and certificate files for secure communication (can be generated using Kubernetes CSR).

### Installation

1. **Download Fugaci**:

   Download the latest release of Fugaci from the [GitHub releases page](https://github.com/macvmio/fugaci/releases):

   ```bash
   curl -L -o /usr/local/bin/fugaci https://github.com/macvmio/fugaci/releases/latest/download/fugaci
   chmod +x /usr/local/bin/fugaci
   ```

2. **Install Curie**:

   Ensure the "curie" binary is installed and available at the specified path in the configuration.

3. **Configure Fugaci**:

   Create a configuration file (e.g., `/etc/fugaci/config.yaml`) with the following content:

   ```yaml
   nodeName: mac-m1
   kubeConfigPath: /Users/your_username/.kube/config
   curieVirtualization:
     binaryPath: /usr/local/bin/curie
     dataRootPath: /Users/your_username/.curie
   internalIP: 192.168.1.99

   TLS:
     # These can be generated with Kubernetes CSR
     keyPath: /Users/your_username/.fugaci/mac-m1.key
     certPath: /Users/your_username/.fugaci/mac-m1.crt
     # This can be taken from the kubeconfig's certificate-authority-data
     certificateAuthorityPath: /Users/your_username/.kube/ca.pem
   ```

   **Note**: Replace `/Users/your_username` with your actual user directory and ensure all paths point to the correct locations on your system.

4. **Bootstrap Fugaci Daemon**:

   Bootstrap and start the Fugaci daemon using the following command:

   ```bash
   sudo /usr/local/bin/fugaci daemon bootstrap
   ```

   This command installs the Fugaci daemon with a `.plist` file for macOS `launchd`, ensuring that Fugaci runs as a background service.

## Usage

Once Fugaci is running, it registers itself as a node in your Kubernetes cluster with a specific taint to prevent standard workloads from being scheduled on it. You can schedule macOS-specific workloads by applying the appropriate tolerations in your pod specifications.

### Example: Scheduling a macOS Pod

Here's a simplified example of how to schedule a macOS pod:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: macos-workload
spec:
  nodeSelector:
    kubernetes.io/os: darwin
  tolerations:
    - key: "fugaci.macvm.io"
      operator: "Equal"
      value: "true"
      effect: "NoSchedule"
  containers:
    - name: macos-container
      image: ghcr.io/macvmio/macos-sonoma:14.5-agent-v1.6
      imagePullPolicy: IfNotPresent
      command:
        - "/bin/bash"
        - "-c"
        - |
          echo "Running macOS workload"
      envFrom:
        - secretRef:
            name: fugaci-ssh-secret
```

**Notes**:

- The `nodeSelector` ensures the pod is scheduled on a node with the `kubernetes.io/os: darwin` label, which Fugaci provides.
- The `tolerations` allow the pod to be scheduled on the node with the specific taint applied by Fugaci.
- Replace `ghcr.io/macvmio/macos-sonoma:14.5-agent-v1.6` with the appropriate macOS image for your workload.
- The `fugaci-ssh-secret` is a reference to a Kubernetes secret containing the username and password, allowing Kubernetes to execute commands via SSH on the newly created VM.

## License

Fugaci is licensed under the Apache License 2.0, allowing you to use, modify, and distribute the software.
See the LICENSE file for more details.

## Credits

Fugaci is developed and maintained by the team at [macvm.io](https://macvm.io). We appreciate contributions from the open-source community.
