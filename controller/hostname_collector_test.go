package main

import (
	"context"
	"testing"

	"go.uber.org/zap"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sfake "k8s.io/client-go/kubernetes/fake"

	gatewayv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayfake "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned/fake"
)

func newTestCollector(objs ...interface{}) *HostnameCollector {
	logger := zap.NewNop()
	k8sClient := k8sfake.NewSimpleClientset()
	gwClient := gatewayfake.NewSimpleClientset()

	for _, obj := range objs {
		switch o := obj.(type) {
		case *networkingv1.Ingress:
			k8sClient.NetworkingV1().Ingresses(o.Namespace).Create(context.Background(), o, metav1.CreateOptions{}) //nolint:errcheck
		case *gatewayv1.Gateway:
			gwClient.GatewayV1().Gateways(o.Namespace).Create(context.Background(), o, metav1.CreateOptions{}) //nolint:errcheck
		case *gatewayv1.HTTPRoute:
			gwClient.GatewayV1().HTTPRoutes(o.Namespace).Create(context.Background(), o, metav1.CreateOptions{}) //nolint:errcheck
		case *gatewayv1.GRPCRoute:
			gwClient.GatewayV1().GRPCRoutes(o.Namespace).Create(context.Background(), o, metav1.CreateOptions{}) //nolint:errcheck
		case *gatewayv1alpha2.TLSRoute:
			gwClient.GatewayV1alpha2().TLSRoutes(o.Namespace).Create(context.Background(), o, metav1.CreateOptions{}) //nolint:errcheck
		}
	}

	return NewHostnameCollector(k8sClient, gwClient, logger)
}

// helpers

func findEntry(entries []HostnameEntry, hostname string) (HostnameEntry, bool) {
	for _, e := range entries {
		if e.Hostname == hostname {
			return e, true
		}
	}
	return HostnameEntry{}, false
}

func assertEntry(t *testing.T, entries []HostnameEntry, hostname string, source Source) {
	t.Helper()
	e, ok := findEntry(entries, hostname)
	if !ok {
		t.Errorf("expected hostname %q not found in entries %v", hostname, entries)
		return
	}
	if e.Source != source {
		t.Errorf("hostname %q: expected source %q, got %q", hostname, source, e.Source)
	}
}

func assertNotPresent(t *testing.T, entries []HostnameEntry, hostname string) {
	t.Helper()
	if _, ok := findEntry(entries, hostname); ok {
		t.Errorf("hostname %q should not be in entries %v", hostname, entries)
	}
}

// ---------------------------------------------------------------------------
// Ingress — source tagging
// ---------------------------------------------------------------------------

func TestCollect_IngressTLSHosts_TaggedAsIngress(t *testing.T) {
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "myapp", Namespace: "default"},
		Spec: networkingv1.IngressSpec{
			TLS: []networkingv1.IngressTLS{
				{Hosts: []string{"tls.example.com"}},
			},
		},
	}
	entries, err := newTestCollector(ing).CollectHostnames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertEntry(t, entries, "tls.example.com", SourceIngress)
}

func TestCollect_IngressRuleHosts_TaggedAsIngress(t *testing.T) {
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "acme", Namespace: "cert-manager"},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{Host: "challenge.example.com"},
			},
		},
	}
	entries, err := newTestCollector(ing).CollectHostnames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertEntry(t, entries, "challenge.example.com", SourceIngress)
}

// ---------------------------------------------------------------------------
// Gateway API — source tagging
// ---------------------------------------------------------------------------

func TestCollect_GatewayListenerHostname_TaggedAsGateway(t *testing.T) {
	hostname := gatewayv1.Hostname("gw.example.com")
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "main", Namespace: "default"},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "example",
			Listeners: []gatewayv1.Listener{
				{Name: "https", Port: 443, Protocol: "HTTPS", Hostname: &hostname},
			},
		},
	}
	entries, err := newTestCollector(gw).CollectHostnames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertEntry(t, entries, "gw.example.com", SourceGateway)
}

func TestCollect_HTTPRouteHostnames_TaggedAsGateway(t *testing.T) {
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "myroute", Namespace: "default"},
		Spec: gatewayv1.HTTPRouteSpec{
			Hostnames: []gatewayv1.Hostname{"http.example.com"},
		},
	}
	entries, err := newTestCollector(route).CollectHostnames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertEntry(t, entries, "http.example.com", SourceGateway)
}

func TestCollect_GRPCRouteHostnames_TaggedAsGateway(t *testing.T) {
	route := &gatewayv1.GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "grpc", Namespace: "default"},
		Spec: gatewayv1.GRPCRouteSpec{
			Hostnames: []gatewayv1.Hostname{"grpc.example.com"},
		},
	}
	entries, err := newTestCollector(route).CollectHostnames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertEntry(t, entries, "grpc.example.com", SourceGateway)
}

func TestCollect_TLSRouteHostnames_TaggedAsGateway(t *testing.T) {
	route := &gatewayv1alpha2.TLSRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "tls", Namespace: "default"},
		Spec: gatewayv1alpha2.TLSRouteSpec{
			Hostnames: []gatewayv1alpha2.Hostname{"tls-route.example.com"},
		},
	}
	entries, err := newTestCollector(route).CollectHostnames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertEntry(t, entries, "tls-route.example.com", SourceGateway)
}

// ---------------------------------------------------------------------------
// Conflict resolution — Gateway wins
// ---------------------------------------------------------------------------

func TestCollect_GatewayWinsOverIngress_OnConflict(t *testing.T) {
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "myapp", Namespace: "default"},
		Spec: networkingv1.IngressSpec{
			TLS: []networkingv1.IngressTLS{{Hosts: []string{"shared.example.com"}}},
		},
	}
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "myroute", Namespace: "default"},
		Spec: gatewayv1.HTTPRouteSpec{
			Hostnames: []gatewayv1.Hostname{"shared.example.com"},
		},
	}
	entries, err := newTestCollector(ing, route).CollectHostnames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// Must appear exactly once and be tagged as Gateway.
	count := 0
	for _, e := range entries {
		if e.Hostname == "shared.example.com" {
			count++
			if e.Source != SourceGateway {
				t.Errorf("shared hostname should be tagged Gateway, got %q", e.Source)
			}
		}
	}
	if count != 1 {
		t.Errorf("shared.example.com should appear exactly once, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// Filtering & sorting
// ---------------------------------------------------------------------------

func TestCollect_WildcardSkipped(t *testing.T) {
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "wildcard", Namespace: "default"},
		Spec: gatewayv1.HTTPRouteSpec{
			Hostnames: []gatewayv1.Hostname{"*.example.com"},
		},
	}
	entries, err := newTestCollector(route).CollectHostnames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertNotPresent(t, entries, "*.example.com")
}

func TestCollect_ResultIsSorted(t *testing.T) {
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "myroute", Namespace: "default"},
		Spec: gatewayv1.HTTPRouteSpec{
			Hostnames: []gatewayv1.Hostname{"z.example.com", "a.example.com", "m.example.com"},
		},
	}
	entries, err := newTestCollector(route).CollectHostnames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(entries); i++ {
		if entries[i].Hostname < entries[i-1].Hostname {
			t.Errorf("entries not sorted at index %d: %v", i, entries)
		}
	}
}

func TestCollect_MultiNamespace(t *testing.T) {
	ing1 := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "app1", Namespace: "ns1"},
		Spec: networkingv1.IngressSpec{
			TLS: []networkingv1.IngressTLS{{Hosts: []string{"ns1.example.com"}}},
		},
	}
	ing2 := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "app2", Namespace: "ns2"},
		Spec: networkingv1.IngressSpec{
			TLS: []networkingv1.IngressTLS{{Hosts: []string{"ns2.example.com"}}},
		},
	}
	entries, err := newTestCollector(ing1, ing2).CollectHostnames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertEntry(t, entries, "ns1.example.com", SourceIngress)
	assertEntry(t, entries, "ns2.example.com", SourceIngress)
}

func TestCollect_Empty(t *testing.T) {
	entries, err := newTestCollector().CollectHostnames(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no entries, got %v", entries)
	}
}
