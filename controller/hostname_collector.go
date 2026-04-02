package main

import (
	"context"
	"regexp"
	"sort"
	"strings"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	gatewayclientset "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
)

// hostnamePattern matches valid DNS hostnames (no wildcards — those are skipped).
var hostnamePattern = regexp.MustCompile(`^[A-Za-z0-9.\-_]+$`)

// HostnameCollector gathers all hostnames that should be hairpin-proxied,
// from every supported resource type across all namespaces.
type HostnameCollector struct {
	k8s    kubernetes.Interface
	gw     gatewayclientset.Interface
	logger *zap.Logger
}

func NewHostnameCollector(k8s kubernetes.Interface, gw gatewayclientset.Interface, logger *zap.Logger) *HostnameCollector {
	return &HostnameCollector{k8s: k8s, gw: gw, logger: logger}
}

// CollectHostnames returns a deduplicated, sorted slice of all hairpin-relevant hostnames.
func (c *HostnameCollector) CollectHostnames(ctx context.Context) ([]string, error) {
	hostSet := make(map[string]struct{})

	// --- Ingress (networking.k8s.io/v1) ---
	if err := c.collectFromIngress(ctx, hostSet); err != nil {
		c.logger.Warn("Could not list Ingress resources (skipping)", zap.Error(err))
	}

	// --- Gateway API: Gateway listeners ---
	if err := c.collectFromGateways(ctx, hostSet); err != nil {
		c.logger.Warn("Could not list Gateway resources (CRD may not be installed, skipping)", zap.Error(err))
	}

	// --- Gateway API: HTTPRoute hostnames ---
	if err := c.collectFromHTTPRoutes(ctx, hostSet); err != nil {
		c.logger.Warn("Could not list HTTPRoute resources (CRD may not be installed, skipping)", zap.Error(err))
	}

	// --- Gateway API: GRPCRoute hostnames (v1 since gateway-api v1.1) ---
	if err := c.collectFromGRPCRoutes(ctx, hostSet); err != nil {
		c.logger.Warn("Could not list GRPCRoute resources (CRD may not be installed, skipping)", zap.Error(err))
	}

	// --- Gateway API: TLSRoute hostnames (v1alpha2) ---
	if err := c.collectFromTLSRoutes(ctx, hostSet); err != nil {
		c.logger.Warn("Could not list TLSRoute resources (CRD may not be installed, skipping)", zap.Error(err))
	}

	hosts := make([]string, 0, len(hostSet))
	for h := range hostSet {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	return hosts, nil
}

// addHost validates and inserts a hostname into the set.
// Wildcard hostnames (*.example.com) are skipped — CoreDNS simple rewrites
// don't support them, and cert-manager HTTP-01 challenges always use exact names.
func (c *HostnameCollector) addHost(hostSet map[string]struct{}, hostname string) {
	if hostname == "" {
		return
	}
	if strings.HasPrefix(hostname, "*") {
		c.logger.Debug("Skipping wildcard hostname", zap.String("hostname", hostname))
		return
	}
	if !hostnamePattern.MatchString(hostname) {
		c.logger.Warn("Skipping hostname with invalid characters", zap.String("hostname", hostname))
		return
	}
	hostSet[hostname] = struct{}{}
}

// collectFromIngress reads TLS hosts and rule hosts from all Ingress resources.
// Rule hosts are included to catch cert-manager HTTP-01 challenge Ingresses which
// may not have a TLS block.
func (c *HostnameCollector) collectFromIngress(ctx context.Context, hostSet map[string]struct{}) error {
	list, err := c.k8s.NetworkingV1().Ingresses("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, ing := range list.Items {
		for _, tls := range ing.Spec.TLS {
			for _, h := range tls.Hosts {
				c.addHost(hostSet, h)
			}
		}
		for _, rule := range ing.Spec.Rules {
			c.addHost(hostSet, rule.Host)
		}
	}
	return nil
}

// collectFromGateways reads listener hostnames from all Gateway resources.
// Each listener can declare a hostname that should be hairpin-proxied.
func (c *HostnameCollector) collectFromGateways(ctx context.Context, hostSet map[string]struct{}) error {
	list, err := c.gw.GatewayV1().Gateways("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, gw := range list.Items {
		for _, listener := range gw.Spec.Listeners {
			if listener.Hostname != nil && *listener.Hostname != "" {
				c.addHost(hostSet, string(*listener.Hostname))
			}
		}
	}
	return nil
}

// collectFromHTTPRoutes reads spec.hostnames from all HTTPRoute resources.
// This is the primary path for cert-manager Gateway API HTTP-01 challenge solver.
func (c *HostnameCollector) collectFromHTTPRoutes(ctx context.Context, hostSet map[string]struct{}) error {
	list, err := c.gw.GatewayV1().HTTPRoutes("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, route := range list.Items {
		for _, h := range route.Spec.Hostnames {
			c.addHost(hostSet, string(h))
		}
	}
	return nil
}

// collectFromGRPCRoutes reads spec.hostnames from all GRPCRoute resources (v1 since gateway-api v1.1).
func (c *HostnameCollector) collectFromGRPCRoutes(ctx context.Context, hostSet map[string]struct{}) error {
	list, err := c.gw.GatewayV1().GRPCRoutes("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, route := range list.Items {
		for _, h := range route.Spec.Hostnames {
			c.addHost(hostSet, string(h))
		}
	}
	return nil
}

// collectFromTLSRoutes reads spec.hostnames from all TLSRoute resources (v1alpha2).
func (c *HostnameCollector) collectFromTLSRoutes(ctx context.Context, hostSet map[string]struct{}) error {
	list, err := c.gw.GatewayV1alpha2().TLSRoutes("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, route := range list.Items {
		for _, h := range route.Spec.Hostnames {
			c.addHost(hostSet, string(h))
		}
	}
	return nil
}
