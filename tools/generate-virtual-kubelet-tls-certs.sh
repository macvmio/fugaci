#!/bin/bash -e

kubectl delete csr virtual-kubelet-csr

# Ensure running in the directory containing the cfssl configuration
cd "$(dirname "$0")"

# Variables
PROFILE="kubelet"  # Name of the profile to use in CSR generation
CONFIG_JSON="kubelet-csr.json"  # CSR configuration file
CSR_NAME="virtual-kubelet-csr"
KEY_FILE="virtual-kubelet-key.pem"
CSR_FILE="virtual-kubelet.csr"
CRT_FILE="virtual-kubelet.crt"

# Step 1: Create a CSR configuration file
cat > ${CONFIG_JSON} <<EOF
{
    "CN": "system:node:mac-m1",
    "key": {
        "algo": "rsa",
        "size": 2048
    },
    "names": [{
        "C": "UK",
        "ST": "London",
        "L": "London",
        "O": "system:nodes",
        "OU": ""
    }],
    "hosts": [
      "192.168.1.99",
      "localhost"
    ]
}
EOF

# Step 2: Generate the CSR and key
cfssl genkey ${CONFIG_JSON} | cfssljson -bare ${CSR_NAME}

# This generates files named `virtual-kubelet-csr-key.pem` (private key) and `virtual-kubelet-csr.csr` (CSR)

# Optional steps, applying the CSR to Kubernetes cluster, require kubectl
# Assuming you have jq and kubectl installed, and your Kubernetes cluster is accessible

# Step 3: Encode the CSR in base64 (single line)
CSR_BASE64=$(cat "${CSR_NAME}.csr" | base64 | tr -d '\n')

# Step 4: Create a CSR resource in Kubernetes
cat <<EOF | kubectl apply -f -
apiVersion: certificates.k8s.io/v1
kind: CertificateSigningRequest
metadata:
  name: ${CSR_NAME}
spec:
  request: ${CSR_BASE64}
  signerName: kubernetes.io/kubelet-serving
  usages:
  - digital signature
  - key encipherment
  - server auth
EOF



# Step 5: Approve the CSR in Kubernetes
kubectl certificate approve ${CSR_NAME}

# Step 6: Retrieve the signed certificate
kubectl get csr ${CSR_NAME} -o jsonpath='{.status.certificate}' | base64 --decode > ${CRT_FILE}

 kubectl get csr ${CSR_NAME} -o yaml

echo "Certificate signing request, key, and certificate (once approved) have been generated:"
echo "CSR: ${CSR_FILE}"
echo "Key: ${KEY_FILE} (named as ${CSR_NAME}-key.pem)"
echo "Certificate (after approval): ${CRT_FILE}"