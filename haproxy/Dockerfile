FROM haproxy:2.3-alpine

EXPOSE 80
EXPOSE 443

# Override TARGET_SERVER to point at your ingress controller or API Gateway service.
# Default targets ingress-nginx in the standard namespace.
ENV TARGET_SERVER=envoy-gateway.envoy-gateway-system.svc.cluster.local

COPY haproxy.cfg /usr/local/etc/haproxy/haproxy.cfg
