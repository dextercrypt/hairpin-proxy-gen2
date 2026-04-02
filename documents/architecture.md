# hairpin-proxy-gen2 — Architecture

## Problem

In Kubernetes, when a Pod tries to reach its own public hostname (e.g. `app.example.com`), DNS resolves to the external LoadBalancer IP. Most cloud load-balancers do not support hairpin NAT, so the connection is dropped or loops back incorrectly.

This breaks:
- **cert-manager HTTP-01 challenges** — the ACME solver pod must reach `http://<domain>/.well-known/acme-challenge/...` from inside the cluster, which fails via the external LB.
- Any workload that calls its own public hostname from inside the cluster.

## Solution

```
Pod → DNS lookup → CoreDNS rewrites hostname → haproxy-{ingress|gateway} Service IP
HAProxy → forwards TCP to your Ingress controller or Gateway API controller
Controller → routes request normally
```

## Components

| Component | Deployment | Role |
|-----------|------------|------|
| `controller` | Deployment (1 replica) | Polls Kubernetes every 15s, collects hostnames, patches CoreDNS ConfigMap |
| `haproxy-ingress` | Deployment + ClusterIP Service | TCP proxy → your Ingress controller (ingress-nginx, Traefik, etc.) |
| `haproxy-gateway` | Deployment + ClusterIP Service | TCP proxy → your Gateway API controller (Envoy Gateway, Istio, etc.) |

All components live in the `hairpin-proxy-gen2` namespace.

## Modes

The controller supports three modes via the `--mode` flag:

| Mode | Watches | HAProxy deployed |
|------|---------|-----------------|
| `ingress` | `networking.k8s.io/v1` Ingress | `haproxy-ingress` only |
| `gateway` | Gateway, HTTPRoute, GRPCRoute, TLSRoute | `haproxy-gateway` only |
| `both` | All of the above | Both HAProxy deployments |

Each hostname is tagged with its source (`ingress` or `gateway`). CoreDNS rewrites route each hostname to the correct HAProxy backend.

## Controller

- Single-replica `Deployment` in `hairpin-proxy-gen2`.
- Polls every 15 seconds (configurable via `--poll-interval`).
- Collects hostnames from:
  - `networking.k8s.io/v1` **Ingress** — `spec.tls[].hosts` and `spec.rules[].host`
  - `gateway.networking.k8s.io/v1` **Gateway** — `spec.listeners[].hostname`
  - `gateway.networking.k8s.io/v1` **HTTPRoute** — `spec.hostnames[]`
  - `gateway.networking.k8s.io/v1` **GRPCRoute** — `spec.hostnames[]`
  - `gateway.networking.k8s.io/v1alpha2` **TLSRoute** — `spec.hostnames[]`
- Wildcard hostnames (`*.example.com`) are skipped — not supported by CoreDNS `rewrite name`.
- If Gateway API CRDs are not installed, the controller warns and continues — no crash.
- Gateway API source wins over Ingress on hostname conflict.

## HAProxy

- TCP mode — no TLS termination, traffic is forwarded as-is.
- `send-proxy` directive passes the PROXY protocol header upstream for correct client IP logging.
- `TARGET_SERVER` env var is set at deploy time to point to your ingress/gateway Service.
- Two independent deployments (`haproxy-ingress`, `haproxy-gateway`) allow each to target a different upstream.

FQDNs used as CoreDNS rewrite targets:
- `haproxy-ingress.hairpin-proxy-gen2.svc.cluster.local`
- `haproxy-gateway.hairpin-proxy-gen2.svc.cluster.local`

## CoreDNS rewrite format

Managed lines are injected immediately after `.:53 {` in the Corefile:

```
.:53 {
    rewrite name app.example.com haproxy-gateway.hairpin-proxy-gen2.svc.cluster.local # managed-by: hairpin-proxy-gen2 | do-not-modify
    rewrite name legacy.example.com haproxy-ingress.hairpin-proxy-gen2.svc.cluster.local # managed-by: hairpin-proxy-gen2 | do-not-modify
    errors
    health
    ...
}
```

On every reconciliation, all managed lines are stripped and re-injected from the current state of Kubernetes resources. The ConfigMap is only updated if the content actually changed.

## RBAC

| Resource | Scope | Permissions |
|----------|-------|-------------|
| Ingress (`networking.k8s.io`) | ClusterRole | get, list, watch |
| Gateway API resources | ClusterRole | get, list, watch |
| `coredns` ConfigMap in `kube-system` | Role (namespaced) | get, update, watch |

RBAC is scoped to the mode — `install-ingress.yaml` does not grant Gateway API permissions, and `install-gateway.yaml` does not grant Ingress permissions.

## Install manifests

| File | Mode | HAProxy deployments |
|------|------|---------------------|
| `install-gateway.yaml` | `--mode=gateway` | `haproxy-gateway` |
| `install-ingress.yaml` | `--mode=ingress` | `haproxy-ingress` |
| `install-both.yaml` | `--mode=both` | Both |
