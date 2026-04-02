package main

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// corefileWithRewrites
// ---------------------------------------------------------------------------

const sampleCorefile = `.:53 {
    errors
    health
    kubernetes cluster.local in-addr.arpa ip6.arpa {
        pods insecure
        fallthrough in-addr.arpa ip6.arpa
    }
    prometheus :9153
    forward . /etc/resolv.conf
    cache 30
    loop
    reload
    loadbalance
}`

func ingressEntry(h string) HostnameEntry { return HostnameEntry{Hostname: h, Source: SourceIngress} }
func gatewayEntry(h string) HostnameEntry { return HostnameEntry{Hostname: h, Source: SourceGateway} }

func TestCorefileWithRewrites_IngressRoutesToNginxBackend(t *testing.T) {
	entries := []HostnameEntry{ingressEntry("legacy.example.com")}
	result, _ := corefileWithRewrites(sampleCorefile, entries)

	if !strings.Contains(result, "rewrite name legacy.example.com "+dnsRewriteDestIngress) {
		t.Errorf("expected ingress rewrite to point to nginx backend, got:\n%s", result)
	}
}

func TestCorefileWithRewrites_GatewayRoutesToEnvoyBackend(t *testing.T) {
	entries := []HostnameEntry{gatewayEntry("app.example.com")}
	result, _ := corefileWithRewrites(sampleCorefile, entries)

	if !strings.Contains(result, "rewrite name app.example.com "+dnsRewriteDestGateway) {
		t.Errorf("expected gateway rewrite to point to envoy backend, got:\n%s", result)
	}
}

func TestCorefileWithRewrites_BothSourcesInSameCorefile(t *testing.T) {
	entries := []HostnameEntry{
		ingressEntry("legacy.example.com"),
		gatewayEntry("app.example.com"),
	}
	result, _ := corefileWithRewrites(sampleCorefile, entries)

	if !strings.Contains(result, "rewrite name legacy.example.com "+dnsRewriteDestIngress) {
		t.Errorf("ingress rewrite missing or wrong backend:\n%s", result)
	}
	if !strings.Contains(result, "rewrite name app.example.com "+dnsRewriteDestGateway) {
		t.Errorf("gateway rewrite missing or wrong backend:\n%s", result)
	}
}

func TestCorefileWithRewrites_InjectsAfterBlock(t *testing.T) {
	entries := []HostnameEntry{
		ingressEntry("api.example.com"),
		gatewayEntry("app.example.com"),
	}
	result, _ := corefileWithRewrites(sampleCorefile, entries)

	lines := strings.Split(result, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == ".:53 {" {
			if i+1 >= len(lines) {
				t.Fatal("no line after .:53 {")
			}
			if !strings.Contains(lines[i+1], "rewrite name") {
				t.Errorf("rewrite not injected right after .:53 {, got: %q", lines[i+1])
			}
			return
		}
	}
	t.Error(".:53 { block not found")
}

func TestCorefileWithRewrites_Idempotent(t *testing.T) {
	entries := []HostnameEntry{ingressEntry("legacy.example.com"), gatewayEntry("app.example.com")}
	first, _ := corefileWithRewrites(sampleCorefile, entries)
	second, _ := corefileWithRewrites(first, entries)

	if strings.TrimSpace(first) != strings.TrimSpace(second) {
		t.Errorf("not idempotent:\nfirst:\n%s\n\nsecond:\n%s", first, second)
	}
}

func TestCorefileWithRewrites_ReplacesOnChange(t *testing.T) {
	first, _ := corefileWithRewrites(sampleCorefile, []HostnameEntry{ingressEntry("old.example.com")})
	second, _ := corefileWithRewrites(first, []HostnameEntry{ingressEntry("new.example.com")})

	if strings.Contains(second, "old.example.com") {
		t.Error("stale rewrite for old.example.com still present")
	}
	if !strings.Contains(second, "new.example.com") {
		t.Error("rewrite for new.example.com missing")
	}
}

func TestCorefileWithRewrites_EmptyEntriesRemovesRewrites(t *testing.T) {
	withRewrites, _ := corefileWithRewrites(sampleCorefile, []HostnameEntry{ingressEntry("app.example.com")})
	cleared, _ := corefileWithRewrites(withRewrites, []HostnameEntry{})

	if strings.Contains(cleared, commentSuffix) {
		t.Error("managed lines still present after clearing entries")
	}
}

func TestCorefileWithRewrites_MissingBlock_ReturnsUnchanged(t *testing.T) {
	malformed := `# no .:53 block here
    errors
    health`

	result, injected := corefileWithRewrites(malformed, []HostnameEntry{ingressEntry("app.example.com")})

	if injected {
		t.Error("expected injected=false for malformed Corefile")
	}
	if strings.Contains(result, "rewrite name") {
		t.Error("injected rewrite into malformed Corefile with no .:53 { block")
	}
}

func TestCorefileWithRewrites_CommentSuffix(t *testing.T) {
	result, _ := corefileWithRewrites(sampleCorefile, []HostnameEntry{ingressEntry("app.example.com")})

	for _, line := range strings.Split(result, "\n") {
		if strings.Contains(line, "rewrite name") {
			if !strings.HasSuffix(strings.TrimSpace(line), commentSuffix) {
				t.Errorf("rewrite line missing comment suffix: %q", line)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// etchostsWithRewrites
// ---------------------------------------------------------------------------

const sampleEtcHosts = `127.0.0.1   localhost
::1         localhost ip6-localhost
10.0.0.1    node1`

func TestEtcHostsWithRewrites_IngressAndGatewayOnSeparateLines(t *testing.T) {
	entries := []HostnameEntry{
		ingressEntry("legacy.example.com"),
		gatewayEntry("app.example.com"),
	}
	result := etchostsWithRewrites(sampleEtcHosts, entries, "10.96.0.10", "10.96.0.20")

	wantIngress := "10.96.0.10\tlegacy.example.com " + commentSuffix
	wantGateway := "10.96.0.20\tapp.example.com " + commentSuffix

	if !strings.Contains(result, wantIngress) {
		t.Errorf("expected ingress line %q in:\n%s", wantIngress, result)
	}
	if !strings.Contains(result, wantGateway) {
		t.Errorf("expected gateway line %q in:\n%s", wantGateway, result)
	}
}

func TestEtcHostsWithRewrites_Idempotent(t *testing.T) {
	entries := []HostnameEntry{ingressEntry("legacy.example.com"), gatewayEntry("app.example.com")}
	first := etchostsWithRewrites(sampleEtcHosts, entries, "10.96.0.10", "10.96.0.20")
	second := etchostsWithRewrites(first, entries, "10.96.0.10", "10.96.0.20")

	if strings.TrimSpace(first) != strings.TrimSpace(second) {
		t.Errorf("not idempotent:\nfirst:\n%s\n\nsecond:\n%s", first, second)
	}
}

func TestEtcHostsWithRewrites_EmptyEntriesRemovesLines(t *testing.T) {
	entries := []HostnameEntry{ingressEntry("legacy.example.com"), gatewayEntry("app.example.com")}
	withLines := etchostsWithRewrites(sampleEtcHosts, entries, "10.96.0.10", "10.96.0.20")
	cleared := etchostsWithRewrites(withLines, []HostnameEntry{}, "10.96.0.10", "10.96.0.20")

	if strings.Contains(cleared, commentSuffix) {
		t.Error("managed lines still present after clearing entries")
	}
}

func TestEtcHostsWithRewrites_PreservesOriginalLines(t *testing.T) {
	entries := []HostnameEntry{ingressEntry("app.example.com")}
	result := etchostsWithRewrites(sampleEtcHosts, entries, "10.96.0.10", "10.96.0.20")

	for _, original := range []string{"127.0.0.1", "::1", "10.0.0.1"} {
		if !strings.Contains(result, original) {
			t.Errorf("original line containing %q was removed", original)
		}
	}
}

func TestEtcHostsWithRewrites_EndsWithNewline(t *testing.T) {
	entries := []HostnameEntry{ingressEntry("app.example.com")}
	result := etchostsWithRewrites(sampleEtcHosts, entries, "10.96.0.10", "10.96.0.20")
	if !strings.HasSuffix(result, "\n") {
		t.Error("result does not end with newline")
	}
}

func TestEtcHostsWithRewrites_OnlyIngressEntries(t *testing.T) {
	entries := []HostnameEntry{ingressEntry("legacy.example.com")}
	result := etchostsWithRewrites(sampleEtcHosts, entries, "10.96.0.10", "10.96.0.20")

	if !strings.Contains(result, "10.96.0.10") {
		t.Error("ingress IP missing")
	}
	if strings.Contains(result, "10.96.0.20") {
		t.Error("gateway IP should not appear when there are no gateway entries")
	}
}

func TestEtcHostsWithRewrites_OnlyGatewayEntries(t *testing.T) {
	entries := []HostnameEntry{gatewayEntry("app.example.com")}
	result := etchostsWithRewrites(sampleEtcHosts, entries, "10.96.0.10", "10.96.0.20")

	if strings.Contains(result, "10.96.0.10") {
		t.Error("ingress IP should not appear when there are no ingress entries")
	}
	if !strings.Contains(result, "10.96.0.20") {
		t.Error("gateway IP missing")
	}
}
