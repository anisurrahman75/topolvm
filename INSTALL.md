# Installing TopoLVM (`ac-v0.41.1-rc.0` / chart `16.2.0`)

This guide installs the fork build of TopoLVM published from
[`anisurrahman75/topolvm`](https://github.com/anisurrahman75/topolvm).

## Published artifacts

| Artifact | Reference |
| --- | --- |
| Controller/node image | `ghcr.io/anisurrahman75/topolvm:0.41.1-rc.0` |
| Image with CSI sidecars | `ghcr.io/anisurrahman75/topolvm-with-sidecar:0.41.1-rc.0` |
| Helm chart (OCI) | `oci://ghcr.io/anisurrahman75/charts/topolvm` (version `16.2.0`, appVersion `0.41.1-rc.0`) |

The chart's default `image.repository` is `ghcr.io/anisurrahman75/topolvm-with-sidecar`
and the image tag defaults to the chart's `appVersion`.

## Prerequisites

- A Kubernetes cluster, v1.33–1.35.
- `kubectl` and `helm` (v3.8+, for OCI support).
- **cert-manager** in the cluster — TopoLVM's mutating webhook uses it for TLS
  (chart default `webhook.certManager: true`).
- **An LVM volume group on every storage node.** The chart's default device-class
  is `ssd` backed by volume group **`myvg1`** (`lvmd.deviceClasses`). The node
  plugin (`lvmd`) will not become Ready until that volume group exists on the node.
  Adjust `lvmd.deviceClasses[].volume-group` to match your environment.

### If the GHCR packages are private

Fresh GHCR packages default to **private**. Either make them Public in the GitHub
package settings (Package → Package settings → Danger Zone → Change visibility), or
authenticate:

```bash
# a GitHub token with read:packages
echo "$GHCR_TOKEN" | helm registry login ghcr.io -u anisurrahman75 --password-stdin

# and a pull secret so the cluster can pull the private image
kubectl create namespace topolvm-system
kubectl -n topolvm-system create secret docker-registry ghcr-cred \
  --docker-server=ghcr.io --docker-username=anisurrahman75 --docker-password="$GHCR_TOKEN"
# then add:  --set image.pullSecrets[0].name=ghcr-cred   to the helm install below
```

## 1. Install cert-manager

```bash
helm repo add jetstack https://charts.jetstack.io && helm repo update
helm install cert-manager jetstack/cert-manager \
  -n cert-manager --create-namespace --set crds.enabled=true
kubectl -n cert-manager rollout status deploy/cert-manager-webhook --timeout=180s
```

## 2. Install TopoLVM

```bash
helm install topolvm oci://ghcr.io/anisurrahman75/charts/topolvm --version 16.2.0 \
  -n topolvm-system --create-namespace
```

Label application namespaces so the webhook mutates their pods (skip
`topolvm-system` and `kube-system`):

```bash
kubectl label namespace default topolvm.io/webhook=ignore-  # example; see docs/
```

To point lvmd at a different volume group:

```bash
helm install topolvm oci://ghcr.io/anisurrahman75/charts/topolvm --version 16.2.0 \
  -n topolvm-system --create-namespace \
  --set lvmd.deviceClasses[0].name=ssd \
  --set lvmd.deviceClasses[0].volume-group=YOUR_VG \
  --set lvmd.deviceClasses[0].default=true \
  --set lvmd.deviceClasses[0].spare-gb=10
```

## 3. Verify

```bash
# controller comes up
kubectl -n topolvm-system rollout status deploy/topolvm-controller --timeout=180s

# node plugin (needs the LVM volume group present)
kubectl -n topolvm-system get pods -o wide

# CRDs and storage class
kubectl get crd | grep topolvm
kubectl get storageclass
```

A quick provisioning smoke test (requires a working volume group on a node):

```bash
kubectl apply -f - <<'EOF'
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: topolvm-test
spec:
  accessModes: ["ReadWriteOnce"]
  resources:
    requests:
      storage: 1Gi
  storageClassName: topolvm-provisioner
EOF

kubectl get pvc topolvm-test -w   # should reach Bound
```

## Uninstall

```bash
helm uninstall topolvm -n topolvm-system
kubectl delete namespace topolvm-system
# CRDs are cluster-scoped; remove them explicitly if desired:
kubectl get crd | grep topolvm.io | awk '{print $1}' | xargs -r kubectl delete crd
```
