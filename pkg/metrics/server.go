package metrics

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog"
)

var (
	tlsCRT = "/etc/secrets/tls.crt"
	tlsKey = "/etc/secrets/tls.key"
)

// RunServer starts the metrics server.
func RunServer(port int, stopCh <-chan struct{}) {
	if port <= 0 {
		klog.Error("invalid port for metric server")
		return
	}

	handler := promhttp.HandlerFor(
		registry,
		promhttp.HandlerOpts{
			ErrorHandling: promhttp.HTTPErrorOnError,
		},
	)

	bindAddr := fmt.Sprintf(":%d", port)
	router := http.NewServeMux()
	router.Handle("/metrics", handler)
	srv := &http.Server{
		Addr:    bindAddr,
		Handler: router,
	}

	go func() {
		err := srv.ListenAndServeTLS(tlsCRT, tlsKey)
		if err != nil && err != http.ErrServerClosed {
			klog.Errorf("error starting metrics server: %v", err)
		}
	}()

	<-stopCh

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		klog.Errorf("error closing metrics server: %v", err)
	}
}

// Degraded increases the degraded counter for provided reason.
func Degraded(reason string) {
	if reason == "" {
		return
	}
	degraded.WithLabelValues(reason).Inc()
}

// StorageReconfigured keeps track of the number of times the operator got its
// underlying storage reconfigured.
func StorageReconfigured() {
	storageReconfigured.Inc()
}
