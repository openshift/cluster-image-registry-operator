package metrics

import (
	"crypto/tls"
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
		Addr:         bindAddr,
		Handler:      router,
		TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){}, // disable HTTP/2
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

// ReportOpenShiftImageStreamTags reports the amount of seen ImageStream tags existing in openshift
// namespaces. Receives the total of 'imported' and 'pushed' image streams tags.
func ReportOpenShiftImageStreamTags(imported float64, pushed float64) {
	imageStreamTags.WithLabelValues("imported", "openshift").Set(imported)
	imageStreamTags.WithLabelValues("pushed", "openshift").Set(pushed)
}

// ReportOtherImageStreamTags reports the amount of seen ImageStream tags existing outside the
// openshift namespaces. Receives the total of 'imported' and 'pushed' image streams tags.
func ReportOtherImageStreamTags(imported float64, pushed float64) {
	imageStreamTags.WithLabelValues("imported", "other").Set(imported)
	imageStreamTags.WithLabelValues("pushed", "other").Set(pushed)
}

// ReportStorageType sets the storage in use.
func ReportStorageType(stype string) {
	storageType.WithLabelValues(stype).Set(1)
}

// AzureKeyCacheHit registers a hit on Azure key cache.
func AzureKeyCacheHit() {
	azurePrimaryKeyCache.With(map[string]string{"result": "hit"}).Inc()
}

// AzureKeyCacheMiss registers a miss on Azure key cache.
func AzureKeyCacheMiss() {
	azurePrimaryKeyCache.With(map[string]string{"result": "miss"}).Inc()
}
