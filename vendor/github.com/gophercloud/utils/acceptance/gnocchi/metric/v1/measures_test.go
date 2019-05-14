// +build acceptance metric measures

package v1

import (
	"testing"

	"github.com/gophercloud/utils/acceptance/clients"
	"github.com/gophercloud/utils/gnocchi/metric/v1/measures"
)

func TestMeasuresCRUD(t *testing.T) {
	client, err := clients.NewGnocchiV1Client()
	if err != nil {
		t.Fatalf("Unable to create a Gnocchi client: %v", err)
	}

	// Create a single metric to test Create measures request.
	metric, err := CreateMetric(t, client)
	if err != nil {
		t.Fatalf("Unable to create a Gnocchi metric: %v", err)
	}
	defer DeleteMetric(t, client, metric.ID)

	// Test Create measures request.
	if err := CreateMeasures(t, client, metric.ID); err != nil {
		t.Fatalf("Unable to create measures inside the Gnocchi metric: %v", err)
	}

	// Check created measures.
	listOpts := measures.ListOpts{
		Refresh: true,
	}
	allPages, err := measures.List(client, metric.ID, listOpts).AllPages()
	if err != nil {
		t.Fatalf("Unable to list measures of the metric %s: %v", metric.ID, err)
	}

	metricMeasures, err := measures.ExtractMeasures(allPages)
	if err != nil {
		t.Fatalf("Unable to extract measures: %v", metricMeasures)
	}

	t.Log(metricMeasures)
}

func TestMeasuresBatchCreateMetrics(t *testing.T) {
	client, err := clients.NewGnocchiV1Client()
	if err != nil {
		t.Fatalf("Unable to create a Gnocchi client: %v", err)
	}

	// Create a couple of metrics to test BatchCreateMetrics requets.
	metricToBatchOne, err := CreateMetric(t, client)
	if err != nil {
		t.Fatalf("Unable to create a Gnocchi metric: %v", err)
	}
	defer DeleteMetric(t, client, metricToBatchOne.ID)

	metricToBatchTwo, err := CreateMetric(t, client)
	if err != nil {
		t.Fatalf("Unable to create a Gnocchi metric: %v", err)
	}
	defer DeleteMetric(t, client, metricToBatchTwo.ID)

	// Test create batch request based on metrics IDs.
	if err := MeasuresBatchCreateMetrics(t, client, metricToBatchOne.ID, metricToBatchTwo.ID); err != nil {
		t.Fatalf("Unable to create measures inside Gnocchi metrics: %v", err)
	}

	// Check measures of each metric after the BatchMetrics request.
	listOpts := measures.ListOpts{
		Refresh: true,
	}
	allPagesMetricOne, err := measures.List(client, metricToBatchOne.ID, listOpts).AllPages()
	if err != nil {
		t.Fatalf("Unable to list measures of the metric %s: %v", metricToBatchOne.ID, err)
	}
	metricOneMeasures, err := measures.ExtractMeasures(allPagesMetricOne)
	if err != nil {
		t.Fatalf("Unable to extract measures: %v", metricOneMeasures)
	}

	t.Logf("Measures for the metric: %s, %v", metricToBatchOne.ID, metricOneMeasures)

	allPagesMetricTwo, err := measures.List(client, metricToBatchTwo.ID, listOpts).AllPages()
	if err != nil {
		t.Fatalf("Unable to list measures of the metric %s: %v", metricToBatchTwo.ID, err)
	}
	metricTwoMeasures, err := measures.ExtractMeasures(allPagesMetricTwo)
	if err != nil {
		t.Fatalf("Unable to extract measures: %v", metricTwoMeasures)
	}

	t.Logf("Measures for the metric: %s, %v", metricToBatchTwo.ID, metricTwoMeasures)
}

func TestMeasuresBatchCreateResourcesMetrics(t *testing.T) {
	client, err := clients.NewGnocchiV1Client()
	if err != nil {
		t.Fatalf("Unable to create a Gnocchi client: %v", err)
	}

	// Create a couple of resources with metrics to test BatchCreateResourcesMetrics requets.
	batchResourcesMetrics, err := CreateResourcesToBatchMeasures(t, client)
	if err != nil {
		t.Fatalf("Unable to create Gnocchi resources and metrics: %v", err)
	}

	// Test create batch request based on resource IDs.
	if err := MeasuresBatchCreateResourcesMetrics(t, client, batchResourcesMetrics); err != nil {
		t.Fatalf("Unable to create measures inside Gnocchi metrics: %v", err)
	}

	// Delete resources.
	for resourceID := range batchResourcesMetrics {
		DeleteResource(t, client, "generic", resourceID)
	}
}
