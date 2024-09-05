


# How to add a secret to Kubernetes

```bash
kubectl create secret generic fugaci-ssh-secret \
  --from-literal=FUGACI_SSH_USERNAME=agent \
  --from-literal=FUGACI_SSH_PASSWORD=password
```

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: testpod4
spec:
  containers:
  - name: curie4
    image: macvm.store/repo/base-runner:1.3
    envFrom:
    - secretRef:
        name: fugaci-ssh-secret
    command: ["/bin/sh", "-c", "env | grep FUGACI"]
```



### Get shell in a container

```shell
kubectl exec --stdin --tty shell-demo -- /bin/bash
```