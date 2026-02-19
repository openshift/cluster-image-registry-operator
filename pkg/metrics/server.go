package metrics

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"

	configv1 "github.com/openshift/api/config/v1"
	"github.com/openshift/library-go/pkg/crypto"
	"k8s.io/klog/v2"
)

// Server represents a metrics server that exposes Prometheus metrics over
// HTTPS with configurable TLS settings.
type Server struct {
	tlsCRT      string
	tlsKey      string
	httpServer  *http.Server
	servingInfo configv1.HTTPServingInfo
}

// NewServer creates a new metrics server.
func NewServer(crt, key string, servingInfo configv1.HTTPServingInfo) *Server {
	handler := promhttp.HandlerFor(
		registry, promhttp.HandlerOpts{
			ErrorHandling: promhttp.HTTPErrorOnError,
		},
	)

	router := http.NewServeMux()
	router.Handle("/metrics", handler)

	return &Server{
		tlsCRT:      crt,
		tlsKey:      key,
		servingInfo: servingInfo,
		httpServer: &http.Server{
			Handler:      router,
			TLSNextProto: map[string]func(*http.Server, *tls.Conn, http.Handler){}, // disable HTTP/2
		},
	}
}

// Run starts the metrics server in a background goroutine. The server
// listens on the configured bind address and serves Prometheus metrics
// at the /metrics endpoint over HTTPS.
func (s *Server) Run() error {
	minTLSVersion, err := crypto.TLSVersion(s.servingInfo.MinTLSVersion)
	if err != nil {
		return fmt.Errorf("failed to parse min tls version: %w", err)
	}

	var suites []uint16
	for _, suite := range s.servingInfo.CipherSuites {
		tmp, err := crypto.CipherSuite(suite)
		if err != nil {
			return fmt.Errorf("failed to parse cipher suite %s: %w", suite, err)
		}
		suites = append(suites, tmp)
	}

	cert, err := tls.LoadX509KeyPair(s.tlsCRT, s.tlsKey)
	if err != nil {
		return fmt.Errorf("failed to load TLS certificate: %w", err)
	}

	listener, err := net.Listen("tcp", s.servingInfo.BindAddress)
	if err != nil {
		return fmt.Errorf("failed to start metrics listener: %w", err)
	}

	go func() {
		tlsConfig := &tls.Config{
			MinVersion:   minTLSVersion,
			CipherSuites: suites,
			Certificates: []tls.Certificate{cert},
		}
		if err := s.httpServer.Serve(tls.NewListener(listener, tlsConfig)); err != nil {
			if err != http.ErrServerClosed {
				klog.Errorf("error starting metrics server: %v", err)
			}
		}
	}()

	return nil
}

// Stop immediately shuts down the metrics server. It is safe to call Stop on a
// server that has not been started. Returns an error if the server fails to
// close.
func (s *Server) Stop() error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Close()
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
