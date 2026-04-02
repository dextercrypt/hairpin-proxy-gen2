package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	gatewayclientset "sigs.k8s.io/gateway-api/pkg/client/clientset/versioned"
)

func main() {
	var (
		pollInterval = flag.Duration("poll-interval", 15*time.Second, "How often to poll Kubernetes resources")
		etchostsPath = flag.String("etc-hosts", "", "Path to writable /etc/hosts file (enables DaemonSet/node mode)")
		modeFlag     = flag.String("mode", "both", "Which resources to watch: gateway (Gateway API only), ingress (Ingress only), both")
	)
	flag.Parse()

	mode, err := ParseMode(*modeFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid --mode %q: must be gateway, ingress, or both\n", *modeFlag)
		os.Exit(1)
	}

	logger, err := zap.NewProduction()
	if err != nil {
		panic(err)
	}
	defer logger.Sync() //nolint:errcheck

	cfg, err := rest.InClusterConfig()
	if err != nil {
		logger.Fatal("Failed to load in-cluster config", zap.Error(err))
	}

	k8sClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		logger.Fatal("Failed to create Kubernetes client", zap.Error(err))
	}

	gwClient, err := gatewayclientset.NewForConfig(cfg)
	if err != nil {
		logger.Fatal("Failed to create Gateway API client", zap.Error(err))
	}

	collector := NewHostnameCollector(k8sClient, gwClient, mode, logger)

	var updater Updater
	if *etchostsPath != "" {
		logger.Info("Starting in /etc/hosts mode (DaemonSet — one instance per Node)",
			zap.String("path", *etchostsPath))
		updater = NewEtcHostsUpdater(*etchostsPath, logger)
	} else {
		logger.Info("Starting in CoreDNS mode (Deployment — one instance per cluster)")
		updater = NewCoreDNSUpdater(k8sClient, logger)
	}

	logger.Info("hairpin-proxy-gen2",
		zap.String("author", "@dextercrypt"),
		zap.String("github", "https://github.com/dextercrypt"),
		zap.String("mode", string(mode)),
	)
	logger.Info("Starting reconciliation loop", zap.Duration("poll_interval", *pollInterval))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	// Run immediately on start, then on every tick.
	reconcile(ctx, collector, updater, logger)

	ticker := time.NewTicker(*pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("Received shutdown signal, exiting")
			return
		case <-ticker.C:
			reconcile(ctx, collector, updater, logger)
		}
	}
}

func reconcile(ctx context.Context, collector *HostnameCollector, updater Updater, logger *zap.Logger) {
	logger.Info("Reconciling hostnames from all sources...")

	entries, err := collector.CollectHostnames(ctx)
	if err != nil {
		logger.Error("Failed to collect hostnames", zap.Error(err))
		return
	}

	ingressCount, gatewayCount := 0, 0
	for _, e := range entries {
		if e.Source == SourceIngress {
			ingressCount++
		} else {
			gatewayCount++
		}
	}
	logger.Info("Collected hostnames",
		zap.Int("total", len(entries)),
		zap.Int("ingress", ingressCount),
		zap.Int("gateway", gatewayCount),
	)

	if err := updater.Update(ctx, entries); err != nil {
		logger.Error("Failed to apply DNS rewrite rules", zap.Error(err))
	}
}
