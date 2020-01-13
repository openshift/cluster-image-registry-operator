package prometheus

import (
	"net/http"

	prometheusclient "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
)

// GetMetricsWithName gets the Prometheus metrics with the given name from the provided URL.
func GetMetricsWithName(url, name string) ([]*prometheusclient.Metric, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	mf := prometheusclient.MetricFamily{}
	decoder := expfmt.NewDecoder(resp.Body, "text/plain")
	for err := decoder.Decode(&mf); err == nil; err = decoder.Decode(&mf) {
		if *mf.Name == name {
			return mf.Metric, nil
		}
	}
	return nil, nil
}
