package metrics

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"io"
	"io/ioutil"
	"math/big"
	"net/http"
	"os"
	"testing"
	"time"

	io_prometheus_client "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

func TestMain(m *testing.M) {
	var err error

	tlsKey, tlsCRT, err = generateTempCertificates()
	if err != nil {
		panic(err)
	}

	// sets the default http client to skip certificate check.
	http.DefaultTransport.(*http.Transport).TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true,
	}

	ch := make(chan struct{})
	go RunServer(5000, ch)

	// give http handlers/server some time to process certificates and
	// get online before running tests.
	time.Sleep(time.Second)

	code := m.Run()
	os.Remove(tlsKey)
	os.Remove(tlsCRT)
	close(ch)
	os.Exit(code)
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

func TestRun(t *testing.T) {
	resp, err := http.Get("https://localhost:5000/metrics")
	if err != nil {
		t.Fatalf("error requesting metrics server: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, received %d instead.", resp.StatusCode)
	}
}

func TestStorageReconfigured(t *testing.T) {
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

			resp, err := http.Get("https://localhost:5000/metrics")
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
