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

func TestCorefileWithRewrites_InjectsAfterBlock(t *testing.T) {
	// Pass pre-sorted hosts (as CollectHostnames always does).
	hosts := []string{"api.example.com", "app.example.com"}
	result := corefileWithRewrites(sampleCorefile, hosts)

	for _, host := range hosts {
		want := "rewrite name " + host + " " + dnsRewriteDest
		if !strings.Contains(result, want) {
			t.Errorf("expected rewrite line for %q, not found in:\n%s", host, result)
		}
	}

	// First rewrite must appear immediately after .:53 {
	lines := strings.Split(result, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == ".:53 {" {
			if i+1 >= len(lines) {
				t.Fatal("no line after .:53 {")
			}
			if !strings.Contains(lines[i+1], "rewrite name api.example.com") {
				t.Errorf("first rewrite not injected right after .:53 {, got: %q", lines[i+1])
			}
			break
		}
	}
}

func TestCorefileWithRewrites_Idempotent(t *testing.T) {
	hosts := []string{"app.example.com"}
	first := corefileWithRewrites(sampleCorefile, hosts)
	second := corefileWithRewrites(first, hosts)

	if strings.TrimSpace(first) != strings.TrimSpace(second) {
		t.Errorf("not idempotent:\nfirst:\n%s\n\nsecond:\n%s", first, second)
	}
}

func TestCorefileWithRewrites_ReplacesOnChange(t *testing.T) {
	first := corefileWithRewrites(sampleCorefile, []string{"old.example.com"})
	second := corefileWithRewrites(first, []string{"new.example.com"})

	if strings.Contains(second, "old.example.com") {
		t.Error("stale rewrite for old.example.com still present after update")
	}
	if !strings.Contains(second, "new.example.com") {
		t.Error("rewrite for new.example.com missing after update")
	}
}

func TestCorefileWithRewrites_EmptyHostsRemovesRewrites(t *testing.T) {
	withRewrites := corefileWithRewrites(sampleCorefile, []string{"app.example.com"})
	cleared := corefileWithRewrites(withRewrites, []string{})

	if strings.Contains(cleared, commentSuffix) {
		t.Error("expected no managed lines after clearing hosts, but found some")
	}
}

func TestCorefileWithRewrites_MissingBlock_ReturnsUnchanged(t *testing.T) {
	malformed := `# no .:53 block here
    errors
    health`

	result := corefileWithRewrites(malformed, []string{"app.example.com"})

	// Should not contain any rewrite since the block was never found
	if strings.Contains(result, "rewrite name") {
		t.Error("injected rewrite into malformed Corefile that has no .:53 { block")
	}
}

func TestCorefileWithRewrites_CommentSuffix(t *testing.T) {
	result := corefileWithRewrites(sampleCorefile, []string{"app.example.com"})

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

func TestEtcHostsWithRewrites_AddsLine(t *testing.T) {
	result := etchostsWithRewrites(sampleEtcHosts, []string{"app.example.com"}, "10.96.0.10")

	want := "10.96.0.10\tapp.example.com " + commentSuffix
	if !strings.Contains(result, want) {
		t.Errorf("expected %q in result:\n%s", want, result)
	}
}

func TestEtcHostsWithRewrites_MultipleHostsOnOneLine(t *testing.T) {
	result := etchostsWithRewrites(sampleEtcHosts, []string{"a.example.com", "b.example.com"}, "10.96.0.10")

	want := "10.96.0.10\ta.example.com b.example.com " + commentSuffix
	if !strings.Contains(result, want) {
		t.Errorf("expected single line with both hosts:\n%s", result)
	}
}

func TestEtcHostsWithRewrites_Idempotent(t *testing.T) {
	hosts := []string{"app.example.com"}
	ip := "10.96.0.10"
	first := etchostsWithRewrites(sampleEtcHosts, hosts, ip)
	second := etchostsWithRewrites(first, hosts, ip)

	if strings.TrimSpace(first) != strings.TrimSpace(second) {
		t.Errorf("not idempotent:\nfirst:\n%s\n\nsecond:\n%s", first, second)
	}
}

func TestEtcHostsWithRewrites_UpdatesIP(t *testing.T) {
	hosts := []string{"app.example.com"}
	first := etchostsWithRewrites(sampleEtcHosts, hosts, "10.96.0.10")
	second := etchostsWithRewrites(first, hosts, "10.96.0.99")

	if strings.Contains(second, "10.96.0.10") {
		t.Error("old IP still present after update")
	}
	if !strings.Contains(second, "10.96.0.99") {
		t.Error("new IP missing after update")
	}
}

func TestEtcHostsWithRewrites_EmptyHostsRemovesLine(t *testing.T) {
	withLine := etchostsWithRewrites(sampleEtcHosts, []string{"app.example.com"}, "10.96.0.10")
	cleared := etchostsWithRewrites(withLine, []string{}, "10.96.0.10")

	if strings.Contains(cleared, commentSuffix) {
		t.Error("managed line still present after clearing hosts")
	}
}

func TestEtcHostsWithRewrites_PreservesOriginalLines(t *testing.T) {
	result := etchostsWithRewrites(sampleEtcHosts, []string{"app.example.com"}, "10.96.0.10")

	for _, original := range []string{"127.0.0.1", "::1", "10.0.0.1"} {
		if !strings.Contains(result, original) {
			t.Errorf("original line containing %q was removed", original)
		}
	}
}

func TestEtcHostsWithRewrites_EndsWithNewline(t *testing.T) {
	result := etchostsWithRewrites(sampleEtcHosts, []string{"app.example.com"}, "10.96.0.10")
	if !strings.HasSuffix(result, "\n") {
		t.Error("result does not end with newline")
	}
}
