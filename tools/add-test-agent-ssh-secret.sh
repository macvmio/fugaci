#!/bin/bash

# This script creates "fugaci-ssh-secret" in default k3s namespace. The secret is needed to boot test VM properly.
# It requires proper KUBECONFIG and kubectl to be available in the environment

kubectl create secret generic fugaci-ssh-secret --from-literal=FUGACI_SSH_USERNAME=agent --from-literal=FUGACI_SSH_PASSWORD=password