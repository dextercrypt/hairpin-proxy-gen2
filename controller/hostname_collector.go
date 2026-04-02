package main

import (
	"context"
	"fmt"
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

// Source identifies which controller owns a hostname, so the updater can
// route CoreDNS rewrites to the correct HAProxy backend.
type Source string

const (
	SourceIngress Source = "ingress"
	SourceGateway Source = "gateway"
)

// Mode controls which resource types the controller watches.
type Mode string

const (
	ModeGateway Mode = "gateway" // Gateway API resources only → haproxy-envoy
	ModeIngress Mode = "ingress" // Ingress resources only    → haproxy-nginx
	ModeBoth    Mode = "both"    // All resources             → routed by source
)

// ParseMode validates and returns a Mode from a string.
func ParseMode(s string) (Mode, error) {
	switch Mode(s) {
	case ModeGateway, ModeIngress, ModeBoth:
		return Mode(s), nil
	default:
		return "", fmt.Errorf("unknown mode %q", s)
	}
}

// HostnameEntry pairs a hostname with the source it was collected from.
type HostnameEntry struct {
	Hostname string
	Source   Source
}

// HostnameCollector gathers all hostnames that should be hairpin-proxied,
// from every supported resource type across all namespaces.
type HostnameCollector struct {
	k8s    kubernetes.Interface
	gw     gatewayclientset.Interface
	mode   Mode
	logger *zap.Logger
}

func NewHostnameCollector(k8s kubernetes.Interface, gw gatewayclientset.Interface, mode Mode, logger *zap.Logger) *HostnameCollector {
	return &HostnameCollector{k8s: k8s, gw: gw, mode: mode, logger: logger}
}

// CollectHostnames returns a deduplicated, sorted slice of HostnameEntry.
// If the same hostname appears in both Ingress and Gateway API resources,
// Gateway takes priority (gen2 promotes Envoy/Gateway API).
func (c *HostnameCollector) CollectHostnames(ctx context.Context) ([]HostnameEntry, error) {
	hostMap := make(map[string]Source)

	// Ingress collected first — Gateway API will overwrite on conflict (Gateway wins).
	if c.mode == ModeIngress || c.mode == ModeBoth {
		if err := c.collectFromIngress(ctx, hostMap); err != nil {
			c.logger.Warn("Could not list Ingress resources (skipping)", zap.Error(err))
		}
	}

	if c.mode == ModeGateway || c.mode == ModeBoth {
		if err := c.collectFromGateways(ctx, hostMap); err != nil {
			c.logger.Warn("Could not list Gateway resources (CRD may not be installed, skipping)", zap.Error(err))
		}
		if err := c.collectFromHTTPRoutes(ctx, hostMap); err != nil {
			c.logger.Warn("Could not list HTTPRoute resources (CRD may not be installed, skipping)", zap.Error(err))
		}
		if err := c.collectFromGRPCRoutes(ctx, hostMap); err != nil {
			c.logger.Warn("Could not list GRPCRoute resources (CRD may not be installed, skipping)", zap.Error(err))
		}
		if err := c.collectFromTLSRoutes(ctx, hostMap); err != nil {
			c.logger.Warn("Could not list TLSRoute resources (CRD may not be installed, skipping)", zap.Error(err))
		}
	}

	entries := make([]HostnameEntry, 0, len(hostMap))
	for hostname, source := range hostMap {
		entries = append(entries, HostnameEntry{Hostname: hostname, Source: source})
	}

	// Sort by hostname for deterministic CoreDNS output.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Hostname < entries[j].Hostname
	})

	return entries, nil
}

// addHost validates and inserts a hostname into the map.
// Gateway API sources overwrite Ingress on conflict.
func (c *HostnameCollector) addHost(hostMap map[string]Source, hostname string, source Source) {
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
	// Gateway API always wins over Ingress on conflict.
	existing, exists := hostMap[hostname]
	if exists && existing == SourceGateway && source == SourceIngress {
		c.logger.Debug("Hostname already claimed by Gateway API, skipping Ingress entry",
			zap.String("hostname", hostname))
		return
	}
	hostMap[hostname] = source
}

func (c *HostnameCollector) collectFromIngress(ctx context.Context, hostMap map[string]Source) error {
	list, err := c.k8s.NetworkingV1().Ingresses("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, ing := range list.Items {
		for _, tls := range ing.Spec.TLS {
			for _, h := range tls.Hosts {
				c.addHost(hostMap, h, SourceIngress)
			}
		}
		for _, rule := range ing.Spec.Rules {
			c.addHost(hostMap, rule.Host, SourceIngress)
		}
	}
	return nil
}

func (c *HostnameCollector) collectFromGateways(ctx context.Context, hostMap map[string]Source) error {
	list, err := c.gw.GatewayV1().Gateways("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, gw := range list.Items {
		for _, listener := range gw.Spec.Listeners {
			if listener.Hostname != nil && *listener.Hostname != "" {
				c.addHost(hostMap, string(*listener.Hostname), SourceGateway)
			}
		}
	}
	return nil
}

func (c *HostnameCollector) collectFromHTTPRoutes(ctx context.Context, hostMap map[string]Source) error {
	list, err := c.gw.GatewayV1().HTTPRoutes("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, route := range list.Items {
		for _, h := range route.Spec.Hostnames {
			c.addHost(hostMap, string(h), SourceGateway)
		}
	}
	return nil
}

func (c *HostnameCollector) collectFromGRPCRoutes(ctx context.Context, hostMap map[string]Source) error {
	list, err := c.gw.GatewayV1().GRPCRoutes("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, route := range list.Items {
		for _, h := range route.Spec.Hostnames {
			c.addHost(hostMap, string(h), SourceGateway)
		}
	}
	return nil
}

func (c *HostnameCollector) collectFromTLSRoutes(ctx context.Context, hostMap map[string]Source) error {
	list, err := c.gw.GatewayV1alpha2().TLSRoutes("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, route := range list.Items {
		for _, h := range route.Spec.Hostnames {
			c.addHost(hostMap, string(h), SourceGateway)
		}
	}
	return nil
}
