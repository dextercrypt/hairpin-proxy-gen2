package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"strings"

	"go.uber.org/zap"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// commentSuffix is appended to every line we own so we can identify and
	// remove them on the next reconciliation pass (idempotent).
	commentSuffix = "# managed-by: hairpin-proxy-gen2 | do-not-modify"

	// dnsRewriteDestIngress is the HAProxy service that forwards to ingress-nginx.
	dnsRewriteDestIngress = "haproxy-ingress.hairpin-proxy-gen2.svc.cluster.local"

	// dnsRewriteDestGateway is the HAProxy service that forwards to envoy-gateway.
	dnsRewriteDestGateway = "haproxy-gateway.hairpin-proxy-gen2.svc.cluster.local"

	corednsNamespace     = "kube-system"
	corednsConfigMapName = "coredns"
	corednsConfigMapKey  = "Corefile"
)

// Updater applies the collected hostname entries to a DNS target.
type Updater interface {
	Update(ctx context.Context, entries []HostnameEntry) error
}

// ---------------------------------------------------------------------------
// CoreDNS updater (Deployment mode)
// ---------------------------------------------------------------------------

// CoreDNSUpdater patches the CoreDNS ConfigMap with name-rewrite rules.
type CoreDNSUpdater struct {
	k8s    kubernetes.Interface
	logger *zap.Logger
}

func NewCoreDNSUpdater(k8s kubernetes.Interface, logger *zap.Logger) *CoreDNSUpdater {
	return &CoreDNSUpdater{k8s: k8s, logger: logger}
}

func (u *CoreDNSUpdater) Update(ctx context.Context, entries []HostnameEntry) error {
	cm, err := u.k8s.CoreV1().ConfigMaps(corednsNamespace).Get(ctx, corednsConfigMapName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get coredns configmap: %w", err)
	}

	oldCorefile := cm.Data[corednsConfigMapKey]
	newCorefile, injected := corefileWithRewrites(oldCorefile, entries)

	if !injected && len(entries) > 0 {
		u.logger.Warn("CoreDNS Corefile is missing '.:53 {' block — hairpin rewrites were NOT injected; check your CoreDNS configuration")
		return nil
	}

	if strings.TrimSpace(oldCorefile) == strings.TrimSpace(newCorefile) {
		u.logger.Info("CoreDNS Corefile is already up-to-date, no changes needed")
		return nil
	}

	cm.Data[corednsConfigMapKey] = newCorefile
	if _, err := u.k8s.CoreV1().ConfigMaps(corednsNamespace).Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("update coredns configmap: %w", err)
	}

	u.logger.Info("Updated CoreDNS Corefile", zap.String("new_corefile", newCorefile))
	return nil
}

// corefileWithRewrites returns the Corefile with all hairpin-proxy rewrite rules
// injected immediately after the `.:53 {` server block opener.
// Each entry is routed to the correct HAProxy backend based on its Source.
// The transformation is idempotent: existing managed lines are removed first.
func corefileWithRewrites(original string, entries []HostnameEntry) (string, bool) {
	lines := strings.Split(strings.TrimSpace(original), "\n")

	// Strip any lines we previously added.
	filtered := lines[:0]
	for _, line := range lines {
		if !strings.HasSuffix(strings.TrimSpace(line), commentSuffix) {
			filtered = append(filtered, line)
		}
	}

	// Build rewrite directives — each hostname points to its own HAProxy backend.
	rewrites := make([]string, 0, len(entries))
	for _, entry := range entries {
		dest := dnsRewriteDestIngress
		if entry.Source == SourceGateway {
			dest = dnsRewriteDestGateway
		}
		rewrites = append(rewrites,
			fmt.Sprintf("    rewrite name %s %s %s", entry.Hostname, dest, commentSuffix))
	}

	// Inject after the `.:53 {` line.
	injected := false
	result := make([]string, 0, len(filtered)+len(rewrites))
	for _, line := range filtered {
		result = append(result, line)
		if strings.TrimSpace(line) == ".:53 {" {
			result = append(result, rewrites...)
			injected = true
		}
	}

	if !injected && len(rewrites) > 0 {
		return strings.Join(filtered, "\n"), false
	}

	return strings.Join(result, "\n"), true
}

// ---------------------------------------------------------------------------
// /etc/hosts updater (DaemonSet mode)
// ---------------------------------------------------------------------------

// EtcHostsUpdater writes a single hairpin-proxy line into /etc/hosts on each
// node. This covers kubelet and the node's container runtime, which bypass
// CoreDNS.
type EtcHostsUpdater struct {
	path   string
	logger *zap.Logger
}

func NewEtcHostsUpdater(path string, logger *zap.Logger) *EtcHostsUpdater {
	if _, err := os.Stat(path); err != nil {
		panic(fmt.Sprintf("etc-hosts path %q does not exist: %v", path, err))
	}
	return &EtcHostsUpdater{path: path, logger: logger}
}

func (u *EtcHostsUpdater) Update(ctx context.Context, entries []HostnameEntry) error {
	// Only resolve backends that have entries — avoids DNS failures for
	// services that aren't deployed (e.g. mode=envoy has no haproxy-ingress).
	var ingressIP, gatewayIP string
	for _, e := range entries {
		if e.Source == SourceIngress && ingressIP == "" {
			ips, err := net.LookupHost(dnsRewriteDestIngress)
			if err != nil || len(ips) == 0 {
				return fmt.Errorf("resolve %s: %w", dnsRewriteDestIngress, err)
			}
			ingressIP = ips[0]
		}
		if e.Source == SourceGateway && gatewayIP == "" {
			ips, err := net.LookupHost(dnsRewriteDestGateway)
			if err != nil || len(ips) == 0 {
				return fmt.Errorf("resolve %s: %w", dnsRewriteDestGateway, err)
			}
			gatewayIP = ips[0]
		}
	}

	data, err := os.ReadFile(u.path)
	if err != nil {
		return fmt.Errorf("read %s: %w", u.path, err)
	}

	oldContent := string(data)
	newContent := etchostsWithRewrites(oldContent, entries, ingressIP, gatewayIP)

	if strings.TrimSpace(oldContent) == strings.TrimSpace(newContent) {
		u.logger.Info("/etc/hosts is already up-to-date, no changes needed")
		return nil
	}

	if err := os.WriteFile(u.path, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("write %s: %w", u.path, err)
	}

	u.logger.Info("Updated /etc/hosts", zap.String("path", u.path), zap.String("new_content", newContent))
	return nil
}

// etchostsWithRewrites returns /etc/hosts content with two managed lines —
// one for Ingress hostnames pointing to haproxy-ingress, one for Gateway API
// hostnames pointing to haproxy-gateway.
func etchostsWithRewrites(original string, entries []HostnameEntry, ingressIP, gatewayIP string) string {
	lines := strings.Split(strings.TrimSpace(original), "\n")

	var originalLines []string
	for _, line := range lines {
		if !strings.HasSuffix(strings.TrimSpace(line), commentSuffix) {
			originalLines = append(originalLines, line)
		}
	}

	var ingressHosts, gatewayHosts []string
	for _, e := range entries {
		if e.Source == SourceIngress {
			ingressHosts = append(ingressHosts, e.Hostname)
		} else {
			gatewayHosts = append(gatewayHosts, e.Hostname)
		}
	}

	result := originalLines
	if len(ingressHosts) > 0 {
		result = append(result, fmt.Sprintf("%s\t%s %s", ingressIP, strings.Join(ingressHosts, " "), commentSuffix))
	}
	if len(gatewayHosts) > 0 {
		result = append(result, fmt.Sprintf("%s\t%s %s", gatewayIP, strings.Join(gatewayHosts, " "), commentSuffix))
	}

	return strings.Join(result, "\n") + "\n"
}
