# TopoLVM (fork) — Quick Start

A fork build of [TopoLVM](https://github.com/topolvm/topolvm) published from
[`anisurrahman75/topolvm`](https://github.com/anisurrahman75/topolvm), with the
Restic/Kopia online-snapshot work. **App `0.41.1-rc.0`, chart `16.2.0`.**

All artifacts below are **public** — no login or pull secret required.

| Artifact | Reference | Public |
| --- | --- | --- |
| Image | `ghcr.io/anisurrahman75/topolvm:0.41.1-rc.0` | ✅ |
| Image (with CSI sidecars) | `ghcr.io/anisurrahman75/topolvm-with-sidecar:0.41.1-rc.0` | ✅ |
| Helm chart | `https://anisurrahman75.github.io/topolvm` (version `16.2.0`) | ✅ |

## Prerequisites

- Kubernetes v1.33–1.35, plus `kubectl` and `helm` (v3.8+).
- **cert-manager** (the webhook needs it).
- **An LVM volume group on each storage node** — the default device-class `ssd`
  uses volume group **`myvg1`**. Change `lvmd.deviceClasses[].volume-group` to match.

## Install

```bash
# 1. cert-manager
helm repo add jetstack https://charts.jetstack.io && helm repo update
helm install cert-manager jetstack/cert-manager \
  -n cert-manager --create-namespace --set crds.enabled=true

# 2. TopoLVM (public Helm repo — no auth)
helm repo add topolvm https://anisurrahman75.github.io/topolvm
helm repo update
helm install topolvm topolvm/topolvm --version 16.2.0 \
  -n topolvm-system --create-namespace
```

Point lvmd at your volume group if it isn't `myvg1`:

```bash
  --set lvmd.deviceClasses[0].name=ssd \
  --set lvmd.deviceClasses[0].volume-group=YOUR_VG \
  --set lvmd.deviceClasses[0].default=true \
  --set lvmd.deviceClasses[0].spare-gb=10
```

## Verify

```bash
kubectl -n topolvm-system rollout status deploy/topolvm-controller
kubectl -n topolvm-system get pods
kubectl get storageclass topolvm-provisioner
```

Provision a volume (needs a working volume group and a consuming pod, because the
StorageClass uses `WaitForFirstConsumer`):

```bash
kubectl apply -f - <<'EOF'
apiVersion: v1
kind: PersistentVolumeClaim
metadata: { name: topolvm-test }
spec:
  accessModes: ["ReadWriteOnce"]
  resources: { requests: { storage: 1Gi } }
  storageClassName: topolvm-provisioner
---
apiVersion: v1
kind: Pod
metadata: { name: topolvm-test-pod }
spec:
  containers:
  - name: app
    image: busybox:1.36
    command: ["sh","-c","sleep 3600"]
    volumeMounts: [{ name: v, mountPath: /data }]
  volumes:
  - name: v
    persistentVolumeClaim: { claimName: topolvm-test }
EOF

kubectl get pvc topolvm-test -w   # -> Bound
```

## Uninstall

```bash
helm uninstall topolvm -n topolvm-system
kubectl delete namespace topolvm-system
```

---

### Alternative: OCI chart

An OCI copy exists at `oci://ghcr.io/anisurrahman75/charts/topolvm` (version `16.2.0`).
It is currently **private**; use the public Helm repo above, or `helm registry login
ghcr.io` first if you prefer OCI.
