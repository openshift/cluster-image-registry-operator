package metrics

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"math/big"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/klog"
)

const (
	defaultTLSCrt = "/etc/secrets/tls.crt"
	defaultTLSKey = "/etc/secrets/tls.key"
)

// RunServer starts the metrics server.
func RunServer(port int, stopCh <-chan struct{}) {
	RunServerWithTLS(port, stopCh, defaultTLSCrt, defaultTLSKey)
}

// RunServerWithTLS starts the metrics server with the provided TLS crt and key files
func RunServerWithTLS(port int, stopCh <-chan struct{}, crt string, key string) {
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
		err := srv.ListenAndServeTLS(crt, key)
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

func ImagePrunerJobCompleted(result string) {
	completedImagePrunerJobs.WithLabelValues(result).Inc()
}

// StartTestMetricsServer launches a local metrics server with a generated, self-signed TLS certificate.
// This method is intended to facilitate testing with Prometheus metrics reported by the operator.
//
// Returns the path to the generated TLS .key and .crt files, and error if any.
func StartTestMetricsServer(port int, stopCh chan struct{}) (tlsKey string, tlsCrt string, err error) {
	tlsKey, tlsCrt, err = generateTempCertificates()
	if err != nil {
		return "", "", err
	}
	go RunServerWithTLS(port, stopCh, tlsCrt, tlsKey)
	return tlsKey, tlsCrt, err
}

func generateTempCertificates() (string, string, error) {
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		return "", "", err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
	}
	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, key.Public(), key)
	if err != nil {
		return "", "", err
	}

	cert, err := ioutil.TempFile("", "testcert-")
	if err != nil {
		return "", "", err
	}
	defer cert.Close()
	pem.Encode(cert, &pem.Block{
		Type:  "CERTIFICATE",
		Bytes: derBytes,
	})

	keyPath, err := ioutil.TempFile("", "testkey-")
	if err != nil {
		return "", "", err
	}
	defer keyPath.Close()
	pem.Encode(keyPath, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})

	return keyPath.Name(), cert.Name(), nil
}
