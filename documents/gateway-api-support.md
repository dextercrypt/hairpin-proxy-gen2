# Gateway API Support

## Why Gateway API?

The Kubernetes Gateway API (`gateway.networking.k8s.io`) is the successor to
Ingress. It's richer, more expressive, and supported by all major ingress
controllers (Envoy Gateway, Cilium Gateway, Kong, Traefik, istio, etc.).

Cert-manager v1.14+ supports the Gateway API `HTTPRoute` as a first-class
solver for HTTP-01 ACME challenges. When you configure cert-manager to use the
Gateway API solver, it creates temporary `HTTPRoute` objects — not `Ingress`
objects — so the original hairpin-proxy (which only watched `Ingress`) would
miss those hostnames.

## Resources we watch

| Resource | API Group | Version | Hostname field |
|----------|-----------|---------|----------------|
| `Gateway` | `gateway.networking.k8s.io` | `v1` | `spec.listeners[].hostname` |
| `HTTPRoute` | `gateway.networking.k8s.io` | `v1` | `spec.hostnames[]` |
| `GRPCRoute` | `gateway.networking.k8s.io` | `v1` | `spec.hostnames[]` |
| `TLSRoute` | `gateway.networking.k8s.io` | `v1alpha2` | `spec.hostnames[]` |

`TCPRoute` and `UDPRoute` have no hostname concept and are intentionally not
watched.

## cert-manager HTTP-01 + Gateway API flow

1. You create a `Certificate` resource pointing to a `Gateway` issuer.
2. cert-manager creates a temporary `HTTPRoute` with the challenge hostname
   (e.g. `app.example.com`) and routes `/.well-known/acme-challenge/*` to its
   solver pod.
3. The hairpin-proxy controller sees the new `HTTPRoute`, picks up
   `app.example.com`, and injects a CoreDNS rewrite within the next poll cycle
   (≤15s by default).
4. CoreDNS now resolves `app.example.com` → `hairpin-proxy.hairpin-proxy.svc.cluster.local`.
5. cert-manager's solver pod (running inside the cluster) makes the HTTP-01
   request. CoreDNS resolves to the hairpin-proxy, HAProxy forwards to your
   Gateway/Ingress, which routes to cert-manager's solver. Challenge succeeds.
6. cert-manager deletes the temporary `HTTPRoute`. On the next poll the
   controller removes the stale CoreDNS rewrite.

## Wildcard hostnames

Wildcard hostnames (`*.example.com`) appear in Gateway listeners and HTTPRoute
specs for catch-all routing but are **skipped** by the controller because:

- CoreDNS's simple `rewrite name` directive does not support wildcards.
- Wildcard certificates require DNS-01 challenges, which do not involve
  in-cluster HTTP requests and therefore don't need hairpin proxying.

## RBAC required

Add these rules to your `ClusterRole`:

```yaml
- apiGroups:
    - gateway.networking.k8s.io
  resources:
    - gateways
    - httproutes
    - grpcroutes
    - tlsroutes
  verbs:
    - get
    - list
    - watch
```

These are already included in the `deploy.yml` provided.

## Graceful degradation

If Gateway API CRDs are not installed in your cluster, the controller logs a
warning for each missing resource type and continues. The controller will still
handle `Ingress` resources correctly. No crash, no restart loop.

## Version compatibility

| gateway-api version | GRPCRoute location |
|--------------------|--------------------|
| v1.0.x             | `v1alpha2`         |
| v1.1.x+            | `v1` (promoted)    |

This controller targets **gateway-api v1.2.0** where `GRPCRoute` is in `v1`.
If you're running an older gateway-api version, update the `collectFromGRPCRoutes`
call in `hostname_collector.go` to use `c.gw.GatewayV1alpha2().GRPCRoutes(...)`.
