# Installing TopoLVM (`ac-v0.41.1-rc.0` / chart `16.2.0`)

This guide installs the fork build of TopoLVM published from
[`anisurrahman75/topolvm`](https://github.com/anisurrahman75/topolvm), which adds
Restic/Kopia-based online snapshot backup & restore.

## Published artifacts — all public, no login or pull secret required

| Artifact | Reference |
| --- | --- |
| Controller/node image | `ghcr.io/anisurrahman75/topolvm:0.41.1-rc.0` |
| Image with CSI sidecars | `ghcr.io/anisurrahman75/topolvm-with-sidecar:0.41.1-rc.0` |
| Helm chart (classic repo) | `https://anisurrahman75.github.io/topolvm` (version `16.2.0`) |
| Helm chart (OCI) | `oci://ghcr.io/anisurrahman75/charts/topolvm` (version `16.2.0`) |
| GitHub releases | app `ac-v0.41.1-rc.0` (lvmd tarball) · chart `topolvm-chart-v16.2.0` |

The chart's default `image.repository` is `ghcr.io/anisurrahman75/topolvm-with-sidecar`
and the image tag defaults to the chart's `appVersion` (`0.41.1-rc.0`).

## Prerequisites

- A Kubernetes cluster, v1.33–1.35.
- `kubectl` and `helm` (v3.8+ for OCI support).
- **cert-manager** in the cluster — TopoLVM's mutating webhook uses it for TLS
  (chart default `webhook.certManager: true`).
- **An LVM volume group on every storage node.** The chart's default device-class
  is `ssd` backed by volume group **`myvg1`** (`lvmd.deviceClasses`). The node
  plugin (`lvmd`) will not become Ready until that volume group exists on the node.
  Adjust `lvmd.deviceClasses[].volume-group` to match your environment.

## 1. Install cert-manager

```bash
helm repo add jetstack https://charts.jetstack.io && helm repo update
helm install cert-manager jetstack/cert-manager \
  -n cert-manager --create-namespace --set crds.enabled=true
kubectl -n cert-manager rollout status deploy/cert-manager-webhook --timeout=180s
```

## 2. Install TopoLVM

**Option A — OCI (simplest, no repo alias needed):**

```bash
helm install topolvm oci://ghcr.io/anisurrahman75/charts/topolvm --version 16.2.0 \
  -n topolvm-system --create-namespace
```

**Option B — classic Helm repo:**

```bash
helm repo add topolvm https://anisurrahman75.github.io/topolvm
helm repo update
helm install topolvm topolvm/topolvm --version 16.2.0 \
  -n topolvm-system --create-namespace
```

> **Note:** if the `topolvm` alias on your machine already points at the official
> repo (`https://topolvm.github.io/topolvm`), add this fork under another alias
> (e.g. `helm repo add topolvm-fork https://anisurrahman75.github.io/topolvm`) —
> the official index tops out at chart 16.1.1 and does not contain 16.2.0.

To point lvmd at a different volume group:

```bash
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
kubectl get storageclass topolvm-provisioner
```

A quick provisioning smoke test (the StorageClass uses `WaitForFirstConsumer`,
so a consuming pod is required for the PVC to bind):

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
---
apiVersion: v1
kind: Pod
metadata:
  name: topolvm-test-pod
spec:
  containers:
  - name: app
    image: busybox:1.36
    command: ["sh","-c","sleep 3600"]
    volumeMounts:
    - { name: vol, mountPath: /data }
  volumes:
  - name: vol
    persistentVolumeClaim: { claimName: topolvm-test }
EOF

kubectl get pvc topolvm-test -w   # should reach Bound
```

## Verified

This release was verified end-to-end on a single-node kind cluster (k8s v1.34):
cert-manager + chart install succeeded (both via OCI and the classic Helm repo,
with **no registry authentication**), the public `ghcr.io/anisurrahman75` images
pulled, controller/node/lvmd pods ran, and a 1Gi PVC bound and mounted (xfs) with
a logical volume carved from the `myvg1` volume group.

> **Testing on kind/containers:** LVM inside a container has no udev, so volume
> creation fails with `device not cleared`. Disable udev in the node's
> `/etc/lvm/lvm.conf` (`activation { udev_sync = 0  udev_rules = 0 }` and
> `devices { obtain_device_list_from_udev = 0 }`). This is not needed on real
> hosts running a normal udev.

## Uninstall

```bash
helm uninstall topolvm -n topolvm-system
kubectl delete namespace topolvm-system
# CRDs are cluster-scoped; remove them explicitly if desired:
kubectl get crd | grep topolvm.io | awk '{print $1}' | xargs -r kubectl delete crd
```
