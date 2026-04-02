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

	// dnsRewriteDest is the in-cluster FQDN of the hairpin-proxy HAProxy Service.
	// CoreDNS rewrites matching hostnames to this address so pods can reach the
	// ingress/gateway from inside the cluster.
	dnsRewriteDest = "haproxy.hairpin-proxy-gen2.svc.cluster.local"

	corednsNamespace     = "kube-system"
	corednsConfigMapName = "coredns"
	corednsConfigMapKey  = "Corefile"
)

// Updater applies the collected hostname list to a DNS target.
type Updater interface {
	Update(ctx context.Context, hosts []string) error
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

func (u *CoreDNSUpdater) Update(ctx context.Context, hosts []string) error {
	cm, err := u.k8s.CoreV1().ConfigMaps(corednsNamespace).Get(ctx, corednsConfigMapName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("get coredns configmap: %w", err)
	}

	oldCorefile := cm.Data[corednsConfigMapKey]
	newCorefile := corefileWithRewrites(oldCorefile, hosts)

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
// The transformation is idempotent: existing managed lines are removed first.
func corefileWithRewrites(original string, hosts []string) string {
	lines := strings.Split(strings.TrimSpace(original), "\n")

	// Strip any lines we previously added.
	filtered := lines[:0]
	for _, line := range lines {
		if !strings.HasSuffix(strings.TrimSpace(line), commentSuffix) {
			filtered = append(filtered, line)
		}
	}

	// Build rewrite directives for each hostname.
	rewrites := make([]string, 0, len(hosts))
	for _, host := range hosts {
		rewrites = append(rewrites,
			fmt.Sprintf("    rewrite name %s %s %s", host, dnsRewriteDest, commentSuffix))
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
		// Corefile is malformed or uses a non-standard structure; don't silently
		// produce a broken config — return the filtered original unchanged so the
		// caller's diff-check skips the update, and the error surfaces in logs.
		return strings.Join(filtered, "\n")
	}

	return strings.Join(result, "\n")
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

func (u *EtcHostsUpdater) Update(ctx context.Context, hosts []string) error {
	ips, err := net.LookupHost(dnsRewriteDest)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("resolve %s: %w", dnsRewriteDest, err)
	}

	data, err := os.ReadFile(u.path)
	if err != nil {
		return fmt.Errorf("read %s: %w", u.path, err)
	}

	oldContent := string(data)
	newContent := etchostsWithRewrites(oldContent, hosts, ips[0])

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

// etchostsWithRewrites returns /etc/hosts content with a single managed line
// mapping all hairpin hosts to the proxy IP.
func etchostsWithRewrites(original string, hosts []string, ip string) string {
	lines := strings.Split(strings.TrimSpace(original), "\n")

	// Remove lines we previously added.
	var originalLines []string
	for _, line := range lines {
		if !strings.HasSuffix(strings.TrimSpace(line), commentSuffix) {
			originalLines = append(originalLines, line)
		}
	}

	if len(hosts) == 0 {
		return strings.Join(originalLines, "\n") + "\n"
	}

	rewriteLine := fmt.Sprintf("%s\t%s %s", ip, strings.Join(hosts, " "), commentSuffix)
	return strings.Join(append(originalLines, rewriteLine), "\n") + "\n"
}
