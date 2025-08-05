#!/bin/bash -e

cd "$(dirname "$0")"

# Input Parameters
NODE_NAME=$1
IP_ADDRESS=$2

# Validate Parameters
if [ -z "$NODE_NAME" ] || [ -z "$IP_ADDRESS" ]; then
  echo "Usage: $0 <Node Name> <IP Address>"
  exit 1
fi


echo "KUBECONFIG is set to ${KUBECONFIG-:none}"
# Variables
PROFILE="kubelet"  # Name of the profile to use in CSR generation
CONFIG_JSON="kubelet-csr.json"  # CSR configuration file
CSR_NAME="${NODE_NAME}"
KEY_FILE="${NODE_NAME}-key"
CSR_FILE="${NODE_NAME}-csr"
CRT_FILE="${NODE_NAME}-crt.pem"


kubectl delete csr "${CSR_NAME}" || true
# Step 1: Create a CSR configuration file

# Step 1: Create a CSR configuration file
cat > ${CONFIG_JSON} <<EOF
{
    "CN": "system:node:${NODE_NAME}",
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
      "${IP_ADDRESS}",
      "localhost"
    ]
}
EOF

echo "CSR configuration file created at ${CONFIG_JSON} for Node: ${NODE_NAME}, IP: ${IP_ADDRESS}"

# Step 2: Generate the CSR and key
cfssl genkey ${CONFIG_JSON} | cfssljson -bare ${CSR_NAME}

# This generates files named `${NODE_NAME}.key.pem` (private key) and `${NODE_NAME}.csr.pem` (CSR)

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

# Wait for the certificate to be issued
echo "Waiting for certificate to be issued..."
for i in {1..15}; do
  CERT=$(kubectl get csr "${CSR_NAME}" -o jsonpath='{.status.certificate}')
  if [[ -n "${CERT}" ]]; then
    echo "Certificate has been issued."
    break
  fi
  echo "Waiting for TLS certificates to be issued"
  sleep 1
done

if [[ -z "${CERT}" ]]; then
  echo "Error: Certificate was not issued for CSR ${CSR_NAME}."
  exit 1
fi

# Step 6: Retrieve the signed certificate
echo "${CERT}" | base64 --decode > "${CRT_FILE}"

# Verify that the certificate file was created
if [[ ! -f "${CRT_FILE}" ]]; then
  echo "Error: Certificate file ${CRT_FILE} was not created."
  exit 1
fi

kubectl get csr ${CSR_NAME} -o yaml

rm "${NODE_NAME}.csr"

echo "Certificate signing request, key, and certificate (once approved) have been generated:"
echo "CSR: ${CSR_FILE}"
echo "Key: ${KEY_FILE} (named as ${CSR_NAME}-key.pem)"
echo "Certificate (after approval): ${CRT_FILE}"
