# hairpin-proxy-gen2 — Architecture

## Problem

In Kubernetes, when a Pod tries to reach its own domain (e.g. `app.example.com`),
DNS resolves to the external IP of the LoadBalancer or Ingress. Most cloud
load-balancers do **not** support hairpin NAT, so the connection from inside the
cluster to the external IP is dropped or routed out and back in incorrectly.

This is especially painful for:
- **cert-manager HTTP-01 challenges** — the ACME solver pod must reach
  `http://<your-domain>/.well-known/acme-challenge/...`, but that loops through
  the external LB and fails.
- Any workload that calls its own public hostname from inside the cluster.

## Solution

```
Pod → DNS lookup → CoreDNS rewrites hostname → hairpin-proxy Service IP
hairpin-proxy (HAProxy) → forwards TCP to Ingress Controller / API Gateway
Ingress Controller → routes request normally
```

Two components:

| Component | What it does |
|-----------|-------------|
| **controller** | Polls Kubernetes, collects hostnames from Ingress + Gateway API, injects `rewrite name` rules into the CoreDNS ConfigMap |
| **haproxy** | TCP proxy listening on `:80` and `:443`. Forwards to your ingress controller or API Gateway service |

## Components

### Controller (Go)

- Runs as a single-replica `Deployment` in the `hairpin-proxy` namespace.
- Polls every 15 seconds (configurable via `--poll-interval`).
- Collects hostnames from:
  - `networking.k8s.io/v1` **Ingress** — TLS hosts + rule hosts
  - `gateway.networking.k8s.io/v1` **Gateway** — listener hostnames
  - `gateway.networking.k8s.io/v1` **HTTPRoute** — spec.hostnames
  - `gateway.networking.k8s.io/v1` **GRPCRoute** — spec.hostnames
  - `gateway.networking.k8s.io/v1alpha2` **TLSRoute** — spec.hostnames
- Rewrites the CoreDNS `Corefile` ConfigMap idempotently.
- Wildcard hostnames (`*.example.com`) are skipped — they require DNS-01 challenges
  and can't be expressed as simple CoreDNS name rewrites.

### HAProxy

- Runs as a `Deployment` with a `ClusterIP` Service named `hairpin-proxy`
  in the `hairpin-proxy` namespace.
- Full FQDN: `hairpin-proxy.hairpin-proxy.svc.cluster.local`
- TCP mode (no TLS termination) — preserves original traffic.
- `send-proxy` directive passes PROXY protocol header to the upstream so it can
  log the correct client IP.
- `TARGET_SERVER` env var points to your ingress/gateway Service.

## CoreDNS rewrite format

Each managed line looks like:

```
    rewrite name app.example.com hairpin-proxy.hairpin-proxy.svc.cluster.local # managed-by hairpin-proxy
```

The comment suffix `# managed-by hairpin-proxy` is used to identify and clean up
stale rules on every reconciliation pass, making the operation fully idempotent.

## Modes

| Mode | Flag | Intended for |
|------|------|-------------|
| CoreDNS | _(default)_ | Single Deployment per cluster — updates CoreDNS ConfigMap |
| /etc/hosts | `--etc-hosts /path` | DaemonSet — patches each Node's `/etc/hosts` (covers kubelet + container runtime) |

## RBAC

The controller needs:
- `ClusterRole`: list/watch `ingresses`, `gateways`, `httproutes`, `grpcroutes`, `tlsroutes`
- `Role` in `kube-system`: get/update the `coredns` ConfigMap

See `deploy.yml` for the full manifest.
