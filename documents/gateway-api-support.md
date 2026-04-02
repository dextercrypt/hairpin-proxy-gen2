# Gateway API Support

## Why Gateway API?

The Kubernetes Gateway API (`gateway.networking.k8s.io`) is the successor to Ingress. It's richer, more expressive, and supported by all major ingress controllers — Envoy Gateway, Istio, Cilium, Kong, Traefik, and others.

cert-manager v1.14+ supports the Gateway API `HTTPRoute` as a first-class solver for HTTP-01 ACME challenges. When cert-manager uses the Gateway API solver, it creates temporary `HTTPRoute` objects — not `Ingress` objects — so the original hairpin-proxy (which only watched `Ingress`) would miss those hostnames entirely.

hairpin-proxy-gen2 watches all Gateway API resource types and routes their hostnames to `haproxy-gateway`, which forwards to your Gateway API controller.

## Resources watched

| Resource | API Group | Version | Hostname field |
|----------|-----------|---------|----------------|
| `Gateway` | `gateway.networking.k8s.io` | `v1` | `spec.listeners[].hostname` |
| `HTTPRoute` | `gateway.networking.k8s.io` | `v1` | `spec.hostnames[]` |
| `GRPCRoute` | `gateway.networking.k8s.io` | `v1` | `spec.hostnames[]` |
| `TLSRoute` | `gateway.networking.k8s.io` | `v1alpha2` | `spec.hostnames[]` |

`TCPRoute` and `UDPRoute` have no hostname concept and are not watched.

## cert-manager HTTP-01 + Gateway API flow

1. You create a `Certificate` resource pointing to a Gateway API issuer.
2. cert-manager creates a temporary `HTTPRoute` with the challenge hostname (e.g. `app.example.com`) and routes `/.well-known/acme-challenge/*` to its solver pod.
3. The hairpin-proxy-gen2 controller sees the new `HTTPRoute`, picks up `app.example.com`, and injects a CoreDNS rewrite within the next poll cycle (≤15s by default):
   ```
   rewrite name app.example.com haproxy-gateway.hairpin-proxy-gen2.svc.cluster.local
   ```
4. CoreDNS now resolves `app.example.com` → `haproxy-gateway.hairpin-proxy-gen2.svc.cluster.local`.
5. cert-manager's solver pod makes the HTTP-01 request. CoreDNS resolves to `haproxy-gateway`, which forwards to your Gateway API controller, which routes to cert-manager's solver. Challenge succeeds.
6. cert-manager deletes the temporary `HTTPRoute`. On the next poll the controller removes the stale CoreDNS rewrite.

## Wildcard hostnames

Wildcard hostnames (`*.example.com`) appear in Gateway listeners and HTTPRoute specs for catch-all routing but are **skipped** by the controller because:

- CoreDNS's `rewrite name` directive does not support wildcards.
- Wildcard certificates require DNS-01 challenges, which do not involve in-cluster HTTP requests and therefore don't need hairpin proxying.

## Graceful degradation

If Gateway API CRDs are not installed in your cluster, the controller logs a warning for each missing resource type and continues running. Ingress resources are still handled correctly. No crash, no restart loop.

This means you can safely run `--mode=both` even if your cluster doesn't have Gateway API CRDs installed yet — the controller will handle Ingress immediately and start picking up Gateway resources as soon as the CRDs appear.

## RBAC

The following ClusterRole rules are required for Gateway API support (already included in `install-gateway.yaml` and `install-both.yaml`):

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

## Version compatibility

This controller targets **gateway-api v1.2.0**:

| Resource | API version in v1.2.0 |
|----------|-----------------------|
| Gateway | `v1` |
| HTTPRoute | `v1` |
| GRPCRoute | `v1` (promoted from `v1alpha2` in v1.1) |
| TLSRoute | `v1alpha2` |

If you're running gateway-api < v1.1, update the `collectFromGRPCRoutes` call in `controller/hostname_collector.go` to use `c.gw.GatewayV1alpha2().GRPCRoutes(...)`.
