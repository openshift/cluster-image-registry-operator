package testing

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/pagination"
	th "github.com/gophercloud/gophercloud/testhelper"
	"github.com/gophercloud/utils/gnocchi/metric/v1/measures"
	fake "github.com/gophercloud/utils/gnocchi/testhelper/client"
)

func TestListMeasures(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	th.Mux.HandleFunc("/v1/metric/9e5a6441-1044-4181-b66e-34e180753040/measures", func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "GET")
		th.TestHeader(t, r, "X-Auth-Token", fake.TokenID)

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		fmt.Fprintf(w, MeasuresListResult)
	})

	metricID := "9e5a6441-1044-4181-b66e-34e180753040"
	startTime := time.Date(2018, 1, 10, 12, 0, 0, 0, time.UTC)
	stopTime := time.Date(2018, 1, 10, 14, 0, 5, 0, time.UTC)
	opts := measures.ListOpts{
		Start:       &startTime,
		Stop:        &stopTime,
		Granularity: "1h",
	}
	expected := ListMeasuresExpected
	pages := 0
	err := measures.List(fake.ServiceClient(), metricID, opts).EachPage(func(page pagination.Page) (bool, error) {
		pages++

		actual, err := measures.ExtractMeasures(page)
		th.AssertNoErr(t, err)

		if len(actual) != 3 {
			t.Fatalf("Expected 2 measures, got %d", len(actual))
		}
		th.CheckDeepEquals(t, expected, actual)

		return true, nil
	})
	th.AssertNoErr(t, err)
	th.CheckEquals(t, 1, pages)
}

func TestCreateMeasures(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	th.Mux.HandleFunc("/v1/metric/9e5a6441-1044-4181-b66e-34e180753040/measures", func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "POST")
		th.TestHeader(t, r, "X-Auth-Token", fake.TokenID)
		th.TestHeader(t, r, "Content-Type", "application/json")
		th.TestHeader(t, r, "Accept", "application/json, */*")
		th.TestJSONRequest(t, r, MeasuresCreateRequest)
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, `{}`)
	})

	firstMeasureTimestamp := time.Date(2018, 1, 18, 12, 31, 0, 0, time.UTC)
	secondMeasureTimestamp := time.Date(2018, 1, 18, 14, 32, 0, 0, time.UTC)
	createOpts := measures.CreateOpts{
		Measures: []measures.MeasureOpts{
			{
				Timestamp: &firstMeasureTimestamp,
				Value:     101.2,
			},
			{
				Timestamp: &secondMeasureTimestamp,
				Value:     102,
			},
		},
	}
	res := measures.Create(fake.ServiceClient(), "9e5a6441-1044-4181-b66e-34e180753040", createOpts)
	th.AssertNoErr(t, res.Err)
}

func TestBatchCreateMetrics(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	th.Mux.HandleFunc("/v1/batch/metrics/measures", func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "POST")
		th.TestHeader(t, r, "X-Auth-Token", fake.TokenID)
		th.TestHeader(t, r, "Content-Type", "application/json")
		th.TestHeader(t, r, "Accept", "application/json, */*")
		th.TestJSONRequest(t, r, MeasuresBatchCreateMetricsRequest)
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, `{}`)
	})

	firstTimestamp := time.Date(2018, 1, 10, 01, 00, 0, 0, time.UTC)
	secondTimestamp := time.Date(2018, 1, 10, 02, 45, 0, 0, time.UTC)
	createOpts := measures.BatchCreateMetricsOpts{
		{
			ID: "777a01d6-4694-49cb-b86a-5ba9fd4e609e",
			Measures: []measures.MeasureOpts{
				{
					Timestamp: &firstTimestamp,
					Value:     200,
				},
				{
					Timestamp: &secondTimestamp,
					Value:     300,
				},
			},
		},
		{
			ID: "6dbc97c5-bfdf-47a2-b184-02e7fa348d21",
			Measures: []measures.MeasureOpts{
				{
					Timestamp: &firstTimestamp,
					Value:     111,
				},
				{
					Timestamp: &secondTimestamp,
					Value:     222,
				},
			},
		},
	}
	res := measures.BatchCreateMetrics(fake.ServiceClient(), createOpts)
	th.AssertNoErr(t, res.Err)
}

func TestBatchCreateResourcesMetrics(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	th.Mux.HandleFunc("/v1/batch/resources/metrics/measures", func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "POST")
		th.TestHeader(t, r, "X-Auth-Token", fake.TokenID)
		th.TestHeader(t, r, "Content-Type", "application/json")
		th.TestHeader(t, r, "Accept", "application/json, */*")
		th.TestJSONRequest(t, r, MeasuresBatchCreateResourcesMetricsRequest)
		w.WriteHeader(http.StatusAccepted)
		fmt.Fprintf(w, `{}`)
	})

	firstTimestamp := time.Date(2018, 1, 20, 12, 30, 0, 0, time.UTC)
	secondTimestamp := time.Date(2018, 1, 20, 13, 15, 0, 0, time.UTC)
	createOpts := measures.BatchCreateResourcesMetricsOpts{
		CreateMetrics: true,
		BatchResourcesMetrics: []measures.BatchResourcesMetricsOpts{
			{
				ResourceID: "75274f99-faf6-4112-a6d5-2794cb07c789",
				ResourcesMetrics: []measures.ResourcesMetricsOpts{
					{
						MetricName:        "network.incoming.bytes.rate",
						ArchivePolicyName: "high",
						Unit:              "B/s",
						Measures: []measures.MeasureOpts{
							{
								Timestamp: &firstTimestamp,
								Value:     1562.82,
							},
							{
								Timestamp: &secondTimestamp,
								Value:     768.1,
							},
						},
					},
					{
						MetricName:        "network.outgoing.bytes.rate",
						ArchivePolicyName: "high",
						Unit:              "B/s",
						Measures: []measures.MeasureOpts{
							{
								Timestamp: &firstTimestamp,
								Value:     273,
							},
							{
								Timestamp: &secondTimestamp,
								Value:     3141.14,
							},
						},
					},
				},
			},
			{
				ResourceID: "23d5d3f7-9dfa-4f73-b72b-8b0b0063ec55",
				ResourcesMetrics: []measures.ResourcesMetricsOpts{
					{
						MetricName:        "disk.write.bytes.rate",
						ArchivePolicyName: "low",
						Unit:              "B/s",
						Measures: []measures.MeasureOpts{
							{
								Timestamp: &firstTimestamp,
								Value:     1237,
							},
							{
								Timestamp: &secondTimestamp,
								Value:     132.12,
							},
						},
					},
				},
			},
		},
	}
	res := measures.BatchCreateResourcesMetrics(fake.ServiceClient(), createOpts)
	th.AssertNoErr(t, res.Err)
}
