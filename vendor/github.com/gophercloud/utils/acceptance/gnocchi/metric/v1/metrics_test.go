// +build acceptance metric metrics

package v1

import (
	"testing"

	"github.com/gophercloud/gophercloud/acceptance/tools"
	"github.com/gophercloud/utils/acceptance/clients"
	"github.com/gophercloud/utils/gnocchi/metric/v1/metrics"
)

func TestMetricsCRUD(t *testing.T) {
	client, err := clients.NewGnocchiV1Client()
	if err != nil {
		t.Fatalf("Unable to create a Gnocchi client: %v", err)
	}

	metric, err := CreateMetric(t, client)
	if err != nil {
		t.Fatalf("Unable to create a Gnocchi metric: %v", err)
	}
	defer DeleteMetric(t, client, metric.ID)

	tools.PrintResource(t, metric)
}

func TestMetricsList(t *testing.T) {
	client, err := clients.NewGnocchiV1Client()
	if err != nil {
		t.Fatalf("Unable to create a Gnocchi client: %v", err)
	}

	listOpts := metrics.ListOpts{}
	allPages, err := metrics.List(client, listOpts).AllPages()
	if err != nil {
		t.Fatalf("Unable to list metrics: %v", err)
	}

	allMetrics, err := metrics.ExtractMetrics(allPages)
	if err != nil {
		t.Fatalf("Unable to extract metrics: %v", err)
	}

	for _, metric := range allMetrics {
		tools.PrintResource(t, metric)
	}
}
