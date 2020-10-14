package metrics

import (
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	"k8s.io/klog/v2"
)

var (
	tlsCRT = "/etc/secrets/tls.crt"
	tlsKey = "/etc/secrets/tls.key"
)

// RunServer starts the metrics server.
func RunServer(port int) {
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

	if err := srv.ListenAndServeTLS(tlsCRT, tlsKey); err != nil {
		klog.Errorf("error starting metrics server: %v", err)
	}
}

// StorageReconfigured keeps track of the number of times the operator got its
// underlying storage reconfigured.
func StorageReconfigured() {
	storageReconfigured.Inc()
}

// ImagePrunerInstallStatus reports the installation state of automatic image pruner CronJob to Prometheus
func ImagePrunerInstallStatus(installed bool, enabled bool) {
	if !installed {
		imagePrunerInstallStatus.Set(0)
		return
	}
	if !enabled {
		imagePrunerInstallStatus.Set(1)
		return
	}
	imagePrunerInstallStatus.Set(2)
}

// AzureKeyCacheHit registers a hit on Azure key cache.
func AzureKeyCacheHit() {
	azurePrimaryKeyCache.With(map[string]string{"result": "hit"}).Inc()
}

// AzureKeyCacheMiss registers a miss on Azure key cache.
func AzureKeyCacheMiss() {
	azurePrimaryKeyCache.With(map[string]string{"result": "miss"}).Inc()
}
