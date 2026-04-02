# hairpin-proxy-gen2

Kubernetes hairpin proxy — Generation 2. Fixes the problem where pods cannot reach their own public hostname from inside the cluster, for both **Ingress** and **Gateway API** resources.

Built in Go. Cert-Manager HTTP-01 ready. Supports ingress-nginx, Envoy Gateway, Istio, Traefik, and any other compliant controller.

---

## The Problem

When a pod makes an HTTP(S) request to its own public hostname (e.g. `api.example.com`), the request exits the cluster, hits the load balancer, and gets dropped — because the load balancer tries to send the traffic back to the same node it came from. This is called the **hairpin NAT problem**.

```
Pod → api.example.com → External LB → Node (source = same node) → DROPPED
```

Cert-Manager's HTTP-01 challenge solver hits this problem: it needs to reach `http://api.example.com/.well-known/acme-challenge/...` from inside the cluster during certificate issuance.

---

## The Solution

hairpin-proxy-gen2 intercepts that traffic before it leaves the cluster:

1. **Controller** watches your Ingress and/or Gateway API resources and collects all hostnames
2. **CoreDNS** is patched with `rewrite name` rules that redirect those hostnames to an internal HAProxy service instead of the external IP
3. **HAProxy** forwards the traffic to your real ingress controller (ingress-nginx, Envoy Gateway, etc.) using the PROXY protocol

```
Pod → api.example.com
       ↓ CoreDNS rewrite
     haproxy-ingress.hairpin-proxy-gen2.svc.cluster.local
       ↓ TCP proxy (PROXY protocol)
     ingress-nginx-controller.ingress-nginx.svc.cluster.local
       ↓
     Your app
```

No external round-trip. No dropped packets. Certificate issuance just works.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  Kubernetes Cluster                                             │
│                                                                 │
│  ┌─────────────┐   watches    ┌──────────────────────────────┐ │
│  │  Controller  │ ──────────▶ │  Ingress resources           │ │
│  │  (Deployment)│             │  Gateway / HTTPRoute /        │ │
│  │              │             │  GRPCRoute / TLSRoute         │ │
│  └──────┬───────┘             └──────────────────────────────┘ │
│         │ patches                                               │
│         ▼                                                       │
│  ┌──────────────┐                                               │
│  │ CoreDNS      │  rewrite name api.example.com →              │
│  │ ConfigMap    │    haproxy-ingress.hairpin-proxy-gen2...      │
│  └──────────────┘                                               │
│                                                                 │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  namespace: hairpin-proxy-gen2                           │  │
│  │                                                          │  │
│  │  haproxy-ingress (Deployment + Service)                  │  │
│  │    └▶ forwards to: ingress-nginx-controller              │  │
│  │                                                          │  │
│  │  haproxy-gateway (Deployment + Service)                  │  │
│  │    └▶ forwards to: envoy-gateway (or Istio, etc.)        │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

### Components

| Component | Type | Role |
|-----------|------|------|
| `controller` | Deployment (1 replica) | Polls k8s resources every 15s, patches CoreDNS ConfigMap |
| `haproxy-ingress` | Deployment + Service | TCP proxy → your Ingress controller |
| `haproxy-gateway` | Deployment + Service | TCP proxy → your Gateway API controller |

### Modes

The controller supports three modes, controlled by the `--mode` flag:

| Mode | Watches | Deploys |
|------|---------|---------|
| `gateway` | Gateway, HTTPRoute, GRPCRoute, TLSRoute | haproxy-gateway only |
| `ingress` | Ingress (networking.k8s.io/v1) | haproxy-ingress only |
| `both` | All of the above | Both HAProxy deployments |

### CoreDNS Rewriting

The controller injects `rewrite name` directives into the CoreDNS Corefile, immediately after the `.:53 {` block opener:

```
.:53 {
    rewrite name api.example.com haproxy-ingress.hairpin-proxy-gen2.svc.cluster.local # managed-by: hairpin-proxy-gen2 | do-not-modify
    rewrite name app.example.com haproxy-gateway.hairpin-proxy-gen2.svc.cluster.local # managed-by: hairpin-proxy-gen2 | do-not-modify
    ...existing plugins...
}
```

Each hostname is routed to the correct HAProxy backend based on which resource type it came from. The update is **idempotent** — managed lines are stripped and re-injected on every reconciliation cycle.

### Gateway API Priority

If the same hostname appears in both an Ingress and a Gateway API resource, Gateway API takes priority (the Gateway rewrite target wins).

---

## Prerequisites

- Kubernetes cluster with `kubectl` access
- CoreDNS as the cluster DNS provider (standard on kubeadm, k3s, EKS, GKE, AKS)
- At least one of:
  - `networking.k8s.io/v1` Ingress controller (ingress-nginx, Traefik, Kong, etc.)
  - Gateway API controller (Envoy Gateway, Istio, Cilium, etc.) with CRDs installed

---

## Installation

### Interactive installer (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/dextercrypt/hairpin-proxy-gen2/main/install.sh | bash
```

The installer will:

1. Check for `kubectl` and `curl`
2. Ask which mode to use (`gateway`, `ingress`, or `both`)
3. Ask for your ingress/gateway controller service address (with sensible defaults)
4. Download and patch the correct manifest
5. Show a summary before applying
6. **Back up your CoreDNS ConfigMap** to `~/.hairpin-proxy-gen2/backups/` before making any changes
7. Apply to your cluster

### Manual installation

Pick the manifest that matches your setup:

```bash
# Gateway API only (Envoy Gateway, Istio, Cilium, etc.)
kubectl apply -f https://raw.githubusercontent.com/dextercrypt/hairpin-proxy-gen2/main/install-gateway.yaml

# Ingress only (ingress-nginx, Traefik, Kong, etc.)
kubectl apply -f https://raw.githubusercontent.com/dextercrypt/hairpin-proxy-gen2/main/install-ingress.yaml

# Both (running both an Ingress controller and a Gateway API controller)
kubectl apply -f https://raw.githubusercontent.com/dextercrypt/hairpin-proxy-gen2/main/install-both.yaml
```

If your controller services are at non-default addresses, patch the `TARGET_SERVER` environment variable in the HAProxy deployment(s) after applying.

**Default target addresses:**

| HAProxy deployment | Default target |
|-------------------|----------------|
| `haproxy-gateway` | `envoy-gateway.envoy-gateway-system.svc.cluster.local` |
| `haproxy-ingress` | `ingress-nginx-controller.ingress-nginx.svc.cluster.local` |

---

## Verifying the installation

```bash
# Check all pods are running
kubectl get all -n hairpin-proxy-gen2

# Watch controller logs
kubectl logs -n hairpin-proxy-gen2 -l app=controller -f

# Confirm CoreDNS was updated
kubectl get configmap coredns -n kube-system -o jsonpath='{.data.Corefile}'
```

You should see lines like:

```
rewrite name api.example.com haproxy-ingress.hairpin-proxy-gen2.svc.cluster.local # managed-by: hairpin-proxy-gen2 | do-not-modify
```

---

## Uninstalling

```bash
# Remove the hairpin-proxy-gen2 namespace and all its resources
kubectl delete namespace hairpin-proxy-gen2

# Remove RBAC
kubectl delete clusterrole hairpin-proxy-gen2
kubectl delete clusterrolebinding hairpin-proxy-gen2

# Restore CoreDNS from backup (the installer saved one at ~/.hairpin-proxy-gen2/backups/)
kubectl apply -f ~/.hairpin-proxy-gen2/backups/coredns-backup-<timestamp>.yaml
```

The controller removes its own managed lines from CoreDNS on the next reconciliation after the namespace is deleted. If you want to force-clean CoreDNS immediately, restore from the backup file saved during installation.

---

## Configuration reference

| Flag | Default | Description |
|------|---------|-------------|
| `--mode` | `both` | Which resources to watch: `gateway`, `ingress`, or `both` |
| `--poll-interval` | `15s` | How often the controller polls Kubernetes resources |
| `--etc-hosts` | _(unset)_ | Path to a writable `/etc/hosts` file; enables node-level rewriting (DaemonSet mode) |

---

## How hostname collection works

The controller scans these resource types depending on the mode:

**Ingress mode:**
- `networking.k8s.io/v1` Ingress — `spec.rules[].host` and `spec.tls[].hosts[]`

**Gateway mode:**
- `gateway.networking.k8s.io/v1` Gateway — `spec.listeners[].hostname`
- `gateway.networking.k8s.io/v1` HTTPRoute — `spec.hostnames[]`
- `gateway.networking.k8s.io/v1` GRPCRoute — `spec.hostnames[]`
- `gateway.networking.k8s.io/v1alpha2` TLSRoute — `spec.hostnames[]`

Wildcard hostnames (`*.example.com`) are skipped — they cannot be used in CoreDNS `rewrite name` directives.

---

## RBAC

hairpin-proxy-gen2 requests the minimum permissions required:

- **ClusterRole** — `get/list/watch` on Ingress and/or Gateway API resources (scoped to mode)
- **Role** in `kube-system` — `get/update/watch` on the `coredns` ConfigMap only

---

## DaemonSet / node mode

If your kubelet or container runtime bypasses CoreDNS, you can run the controller as a DaemonSet and pass `--etc-hosts=/path/to/node/etc/hosts` to write directly to each node's `/etc/hosts` file. In this mode, the controller writes one line per backend (ingress hosts and gateway hosts separately) instead of patching CoreDNS.

---

## Credits

Built by [@dextercrypt](https://github.com/dextercrypt). Inspired by the original [hairpin-proxy](https://github.com/compumike/hairpin-proxy) by [@compumike](https://github.com/compumike).
