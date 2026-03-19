package metrics

import (
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"testing"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"k8s.io/client-go/util/cert"
)

func generateTempCertificates(t *testing.T) (string, string) {
	certPEM, keyPEM, err := cert.GenerateSelfSignedCertKey("localhost", nil, nil)
	if err != nil {
		t.Fatalf("failed to generate self-signed certificate: %v", err)
	}

	certFile, err := os.CreateTemp("", "testcert-")
	if err != nil {
		t.Fatalf("failed to create temp cert file: %v", err)
	}
	certPath := certFile.Name()
	certFile.Close()
	t.Cleanup(func() {
		_ = os.Remove(certPath)
	})

	keyFile, err := os.CreateTemp("", "testkey-")
	if err != nil {
		t.Fatalf("failed to create temp key file: %v", err)
	}
	keyPath := keyFile.Name()
	keyFile.Close()
	t.Cleanup(func() {
		_ = os.Remove(keyPath)
	})

	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		t.Fatalf("failed to write cert file: %v", err)
	}

	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	return keyPath, certPath
}

func TestStorageReconfigured(t *testing.T) {
	tlsKey, tlsCRT := generateTempCertificates(t)
	servingInfo := configv1.HTTPServingInfo{
		ServingInfo: configv1.ServingInfo{BindAddress: "localhost:5000"},
	}

	server := NewServer(tlsCRT, tlsKey, servingInfo)

	if err := server.Run(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		if err := server.Stop(); err != nil {
			t.Errorf("failed to stop metrics server: %v", err)
		}
	}()
	time.Sleep(100 * time.Millisecond)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		Timeout: 100 * time.Millisecond,
	}

	metricName := "image_registry_operator_storage_reconfigured_total"
	for _, tt := range []struct {
		name string
		iter int
		expt float64
	}{
		{
			name: "zeroed",
			iter: 0,
			expt: 0,
		},
		{
			name: "increase to five",
			iter: 5,
			expt: 5,
		},
		{
			name: "increase to ten",
			iter: 5,
			expt: 10,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < tt.iter; i++ {
				StorageReconfigured()
			}

			resp, err := client.Get("https://localhost:5000/metrics")
			if err != nil {
				t.Fatalf("error requesting metrics server: %v", err)
			}

			metrics := findMetricsByCounter(resp.Body, metricName)
			if len(metrics) == 0 {
				t.Fatal("unable to locate metric", metricName)
			}

			val := *metrics[0].Counter.Value
			if val != tt.expt {
				t.Errorf("expected %.0f, found %.0f", tt.expt, val)
			}
		})
	}
}

func TestImagePrunerInstallStatus(t *testing.T) {
	tlsKey, tlsCRT := generateTempCertificates(t)
	servingInfo := configv1.HTTPServingInfo{
		ServingInfo: configv1.ServingInfo{BindAddress: "localhost:5000"},
	}

	server := NewServer(tlsCRT, tlsKey, servingInfo)

	if err := server.Run(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer func() {
		if err := server.Stop(); err != nil {
			t.Errorf("failed to stop metrics server: %v", err)
		}
	}()
	time.Sleep(100 * time.Millisecond)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
		Timeout: 100 * time.Millisecond,
	}

	metricName := "image_registry_operator_image_pruner_install_status"
	testCases := []struct {
		name      string
		installed bool
		enabled   bool
	}{
		{
			name:      "not installed",
			installed: false,
			enabled:   false,
		},
		{
			name:      "suspended",
			installed: true,
			enabled:   false,
		},
		{
			name:      "enabled",
			installed: true,
			enabled:   true,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ImagePrunerInstallStatus(tc.installed, tc.enabled)

			resp, err := client.Get("https://localhost:5000/metrics")
			if err != nil {
				t.Fatalf("error requesting metrics server: %v", err)
			}

			metrics := findMetricsByCounter(resp.Body, metricName)
			if len(metrics) == 0 {
				t.Fatal("unable to locate metric", metricName)
			}

			for _, m := range metrics {
				if !tc.installed && m.Gauge.GetValue() != 0 {
					t.Errorf("expected metric %s to be 0, got %f", metricName, m.Gauge.GetValue())
				}
				if tc.installed && !tc.enabled && m.Gauge.GetValue() != 1 {
					t.Errorf("expected metric %s to be 1, got %f", metricName, m.Gauge.GetValue())
				}
				if tc.installed && tc.enabled && m.Gauge.GetValue() != 2 {
					t.Errorf("expected metric %s to be 2, got %f", metricName, m.Gauge.GetValue())
				}
			}
		})
	}
}

func findMetricsByCounter(buf io.ReadCloser, name string) []*io_prometheus_client.Metric {
	defer buf.Close()
	mf := io_prometheus_client.MetricFamily{}
	decoder := expfmt.NewDecoder(buf, "text/plain")
	for err := decoder.Decode(&mf); err == nil; err = decoder.Decode(&mf) {
		if *mf.Name == name {
			return mf.Metric
		}
	}
	return nil
}

func TestTLSConfiguration(t *testing.T) {
	tlsKey, tlsCRT := generateTempCertificates(t)
	for _, tc := range []struct {
		name              string
		servingInfo       configv1.HTTPServingInfo
		clientTLSConfig   *tls.Config
		expectServerError bool
		expectSuccess     bool
	}{
		{
			name: "default server config accepts TLS 1.2 client",
			servingInfo: configv1.HTTPServingInfo{
				ServingInfo: configv1.ServingInfo{
					BindAddress: "localhost:5000",
				},
			},
			clientTLSConfig: &tls.Config{
				InsecureSkipVerify: true,
				MinVersion:         tls.VersionTLS12,
				MaxVersion:         tls.VersionTLS12,
			},
			expectSuccess: true,
		},
		{
			name: "default server config accepts TLS 1.3 client",
			servingInfo: configv1.HTTPServingInfo{
				ServingInfo: configv1.ServingInfo{
					BindAddress: "localhost:5000",
				},
			},
			clientTLSConfig: &tls.Config{
				InsecureSkipVerify: true,
				MinVersion:         tls.VersionTLS13,
				MaxVersion:         tls.VersionTLS13,
			},
			expectSuccess: true,
		},
		{
			name: "TLS 1.2 server accepts TLS 1.2 client",
			servingInfo: configv1.HTTPServingInfo{
				ServingInfo: configv1.ServingInfo{
					BindAddress:   "localhost:5000",
					MinTLSVersion: "VersionTLS12",
				},
			},
			clientTLSConfig: &tls.Config{
				InsecureSkipVerify: true,
				MinVersion:         tls.VersionTLS12,
				MaxVersion:         tls.VersionTLS12,
			},
			expectSuccess: true,
		},
		{
			name: "TLS 1.2 server accepts TLS 1.3 client",
			servingInfo: configv1.HTTPServingInfo{
				ServingInfo: configv1.ServingInfo{
					BindAddress:   "localhost:5000",
					MinTLSVersion: "VersionTLS12",
				},
			},
			clientTLSConfig: &tls.Config{
				InsecureSkipVerify: true,
				MinVersion:         tls.VersionTLS13,
				MaxVersion:         tls.VersionTLS13,
			},
			expectSuccess: true,
		},
		{
			name: "TLS 1.3 server accepts TLS 1.3 client",
			servingInfo: configv1.HTTPServingInfo{
				ServingInfo: configv1.ServingInfo{
					BindAddress:   "localhost:5000",
					MinTLSVersion: "VersionTLS13",
				},
			},
			clientTLSConfig: &tls.Config{
				InsecureSkipVerify: true,
				MinVersion:         tls.VersionTLS13,
				MaxVersion:         tls.VersionTLS13,
			},
			expectSuccess: true,
		},
		{
			name: "TLS 1.3 server rejects TLS 1.2 client",
			servingInfo: configv1.HTTPServingInfo{
				ServingInfo: configv1.ServingInfo{
					BindAddress:   "localhost:5000",
					MinTLSVersion: "VersionTLS13",
				},
			},
			clientTLSConfig: &tls.Config{
				InsecureSkipVerify: true,
				MinVersion:         tls.VersionTLS12,
				MaxVersion:         tls.VersionTLS12,
			},
			expectSuccess: false,
		},
		{
			name: "invalid MinTLSVersion returns error",
			servingInfo: configv1.HTTPServingInfo{
				ServingInfo: configv1.ServingInfo{
					BindAddress:   "localhost:5008",
					MinTLSVersion: "InvalidTLSVersion",
				},
			},
			expectServerError: true,
		},
		{
			name: "invalid CipherSuites returns error",
			servingInfo: configv1.HTTPServingInfo{
				ServingInfo: configv1.ServingInfo{
					BindAddress:   "localhost:5009",
					MinTLSVersion: "VersionTLS12",
					CipherSuites:  []string{"INVALID_CIPHER_SUITE"},
				},
			},
			expectServerError: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := NewServer(tlsCRT, tlsKey, tc.servingInfo)

			err := server.Run()
			if tc.expectServerError {
				if err == nil {
					t.Error("expected server to return error, but it succeeded")
				}
				return
			}
			if err != nil {
				t.Errorf("failed to start server: %v", err)
				return
			}

			defer func() {
				if err := server.Stop(); err != nil {
					t.Errorf("failed to stop metrics server: %v", err)
				}
			}()
			time.Sleep(100 * time.Millisecond)

			client := &http.Client{
				Transport: &http.Transport{TLSClientConfig: tc.clientTLSConfig},
				Timeout:   100 * time.Millisecond,
			}

			url := fmt.Sprintf("https://%s/metrics", tc.servingInfo.BindAddress)
			resp, err := client.Get(url)
			if tc.expectSuccess {
				if err != nil {
					t.Errorf("expected successful connection but got error: %v", err)
					return
				}
				resp.Body.Close()
				return
			}

			if err == nil {
				t.Error("expected connection to fail, but it succeeded")
			}
		})
	}
}
