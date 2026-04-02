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

func newTestCollector(k8sObjs ...interface{}) (*HostnameCollector, *k8sfake.Clientset, *gatewayfake.Clientset) {
	logger := zap.NewNop()
	k8sClient := k8sfake.NewSimpleClientset()
	gwClient := gatewayfake.NewSimpleClientset()

	// Pre-populate k8s objects
	for _, obj := range k8sObjs {
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

	return NewHostnameCollector(k8sClient, gwClient, logger), k8sClient, gwClient
}

// ---------------------------------------------------------------------------
// Ingress collection
// ---------------------------------------------------------------------------

func TestCollect_IngressTLSHosts(t *testing.T) {
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "myapp", Namespace: "default"},
		Spec: networkingv1.IngressSpec{
			TLS: []networkingv1.IngressTLS{
				{Hosts: []string{"tls.example.com", "tls2.example.com"}},
			},
		},
	}
	c, _, _ := newTestCollector(ing)
	hosts, err := c.CollectHostnames(context.Background())
	assertNoError(t, err)
	assertContains(t, hosts, "tls.example.com")
	assertContains(t, hosts, "tls2.example.com")
}

func TestCollect_IngressRuleHosts(t *testing.T) {
	// Rule hosts (no TLS block) — cert-manager HTTP-01 challenge Ingresses look like this
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "acme-solver", Namespace: "cert-manager"},
		Spec: networkingv1.IngressSpec{
			Rules: []networkingv1.IngressRule{
				{Host: "challenge.example.com"},
			},
		},
	}
	c, _, _ := newTestCollector(ing)
	hosts, err := c.CollectHostnames(context.Background())
	assertNoError(t, err)
	assertContains(t, hosts, "challenge.example.com")
}

func TestCollect_IngressEmpty(t *testing.T) {
	c, _, _ := newTestCollector()
	hosts, err := c.CollectHostnames(context.Background())
	assertNoError(t, err)
	if len(hosts) != 0 {
		t.Errorf("expected no hosts, got %v", hosts)
	}
}

// ---------------------------------------------------------------------------
// Gateway collection
// ---------------------------------------------------------------------------

func TestCollect_GatewayListenerHostname(t *testing.T) {
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
	c, _, _ := newTestCollector(gw)
	hosts, err := c.CollectHostnames(context.Background())
	assertNoError(t, err)
	assertContains(t, hosts, "gw.example.com")
}

func TestCollect_GatewayNilHostname_Skipped(t *testing.T) {
	gw := &gatewayv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "main", Namespace: "default"},
		Spec: gatewayv1.GatewaySpec{
			GatewayClassName: "example",
			Listeners: []gatewayv1.Listener{
				{Name: "http", Port: 80, Protocol: "HTTP"}, // no Hostname field
			},
		},
	}
	c, _, _ := newTestCollector(gw)
	hosts, err := c.CollectHostnames(context.Background())
	assertNoError(t, err)
	if len(hosts) != 0 {
		t.Errorf("expected no hosts from nil listener hostname, got %v", hosts)
	}
}

// ---------------------------------------------------------------------------
// HTTPRoute collection
// ---------------------------------------------------------------------------

func TestCollect_HTTPRouteHostnames(t *testing.T) {
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "myroute", Namespace: "default"},
		Spec: gatewayv1.HTTPRouteSpec{
			Hostnames: []gatewayv1.Hostname{"http.example.com", "http2.example.com"},
		},
	}
	c, _, _ := newTestCollector(route)
	hosts, err := c.CollectHostnames(context.Background())
	assertNoError(t, err)
	assertContains(t, hosts, "http.example.com")
	assertContains(t, hosts, "http2.example.com")
}

// ---------------------------------------------------------------------------
// GRPCRoute collection
// ---------------------------------------------------------------------------

func TestCollect_GRPCRouteHostnames(t *testing.T) {
	route := &gatewayv1.GRPCRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "grpcroute", Namespace: "default"},
		Spec: gatewayv1.GRPCRouteSpec{
			Hostnames: []gatewayv1.Hostname{"grpc.example.com"},
		},
	}
	c, _, _ := newTestCollector(route)
	hosts, err := c.CollectHostnames(context.Background())
	assertNoError(t, err)
	assertContains(t, hosts, "grpc.example.com")
}

// ---------------------------------------------------------------------------
// TLSRoute collection
// ---------------------------------------------------------------------------

func TestCollect_TLSRouteHostnames(t *testing.T) {
	route := &gatewayv1alpha2.TLSRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "tlsroute", Namespace: "default"},
		Spec: gatewayv1alpha2.TLSRouteSpec{
			CommonRouteSpec: gatewayv1alpha2.CommonRouteSpec{},
			Hostnames:       []gatewayv1alpha2.Hostname{"tls-route.example.com"},
		},
	}
	c, _, _ := newTestCollector(route)
	hosts, err := c.CollectHostnames(context.Background())
	assertNoError(t, err)
	assertContains(t, hosts, "tls-route.example.com")
}

// ---------------------------------------------------------------------------
// Hostname validation / filtering
// ---------------------------------------------------------------------------

func TestCollect_WildcardHostname_Skipped(t *testing.T) {
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "wildcard", Namespace: "default"},
		Spec: gatewayv1.HTTPRouteSpec{
			Hostnames: []gatewayv1.Hostname{"*.example.com"},
		},
	}
	c, _, _ := newTestCollector(route)
	hosts, err := c.CollectHostnames(context.Background())
	assertNoError(t, err)
	assertNotContains(t, hosts, "*.example.com")
}

func TestCollect_Deduplication(t *testing.T) {
	// Same hostname appears in both Ingress and HTTPRoute
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
	c, _, _ := newTestCollector(ing, route)
	hosts, err := c.CollectHostnames(context.Background())
	assertNoError(t, err)

	count := 0
	for _, h := range hosts {
		if h == "shared.example.com" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected shared.example.com exactly once, got %d times in %v", count, hosts)
	}
}

func TestCollect_ResultIsSorted(t *testing.T) {
	route := &gatewayv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "myroute", Namespace: "default"},
		Spec: gatewayv1.HTTPRouteSpec{
			Hostnames: []gatewayv1.Hostname{"z.example.com", "a.example.com", "m.example.com"},
		},
	}
	c, _, _ := newTestCollector(route)
	hosts, err := c.CollectHostnames(context.Background())
	assertNoError(t, err)

	for i := 1; i < len(hosts); i++ {
		if hosts[i] < hosts[i-1] {
			t.Errorf("hosts not sorted at index %d: %v", i, hosts)
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
	c, _, _ := newTestCollector(ing1, ing2)
	hosts, err := c.CollectHostnames(context.Background())
	assertNoError(t, err)
	assertContains(t, hosts, "ns1.example.com")
	assertContains(t, hosts, "ns2.example.com")
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func assertContains(t *testing.T, hosts []string, want string) {
	t.Helper()
	for _, h := range hosts {
		if h == want {
			return
		}
	}
	t.Errorf("expected %q in hosts %v", want, hosts)
}

func assertNotContains(t *testing.T, hosts []string, unwanted string) {
	t.Helper()
	for _, h := range hosts {
		if h == unwanted {
			t.Errorf("unexpected %q found in hosts %v", unwanted, hosts)
			return
		}
	}
}
