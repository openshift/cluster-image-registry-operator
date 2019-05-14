package testing

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/pagination"
	th "github.com/gophercloud/gophercloud/testhelper"
	"github.com/gophercloud/utils/gnocchi/metric/v1/archivepolicies"
	"github.com/gophercloud/utils/gnocchi/metric/v1/metrics"
	"github.com/gophercloud/utils/gnocchi/metric/v1/resources"
	fake "github.com/gophercloud/utils/gnocchi/testhelper/client"
)

func TestList(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	th.Mux.HandleFunc("/v1/metric", func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "GET")
		th.TestHeader(t, r, "X-Auth-Token", fake.TokenID)

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		fmt.Fprintf(w, MetricsListResult)
	})

	count := 0

	metrics.List(fake.ServiceClient(), metrics.ListOpts{}).EachPage(func(page pagination.Page) (bool, error) {
		count++
		actual, err := metrics.ExtractMetrics(page)
		if err != nil {
			t.Errorf("Failed to extract metrics: %v", err)
			return false, nil
		}

		expected := []metrics.Metric{
			Metric1,
			Metric2,
		}

		th.CheckDeepEquals(t, expected, actual)

		return true, nil
	})

	if count != 1 {
		t.Errorf("Expected 1 page, got %d", count)
	}
}

func TestGet(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	th.Mux.HandleFunc("/v1/metric/0ddf61cf-3747-4f75-bf13-13c28ff03ae3", func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "GET")
		th.TestHeader(t, r, "X-Auth-Token", fake.TokenID)

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		fmt.Fprintf(w, MetricGetResult)
	})

	s, err := metrics.Get(fake.ServiceClient(), "0ddf61cf-3747-4f75-bf13-13c28ff03ae3").Extract()
	th.AssertNoErr(t, err)

	th.AssertDeepEquals(t, s.ArchivePolicy, archivepolicies.ArchivePolicy{
		AggregationMethods: []string{
			"mean",
			"sum",
		},
		BackWindow: 12,
		Definition: []archivepolicies.ArchivePolicyDefinition{
			{
				Granularity: "1:00:00",
				Points:      2160,
				TimeSpan:    "90 days, 0:00:00",
			},
			{
				Granularity: "1 day, 0:00:00",
				Points:      200,
				TimeSpan:    "200 days, 0:00:00",
			},
		},
		Name: "not_so_precise",
	})
	th.AssertEquals(t, s.CreatedByProjectID, "c6b68a6b413648b0a0eb191bf3222f4d")
	th.AssertEquals(t, s.CreatedByUserID, "cb072aacdb494419aeeba5f1c62d1a65")
	th.AssertEquals(t, s.Creator, "cb072aacdb494419aeeba5f1c62d1a65:c6b68a6b413648b0a0eb191bf3222f4d")
	th.AssertEquals(t, s.ID, "0ddf61cf-3747-4f75-bf13-13c28ff03ae3")
	th.AssertEquals(t, s.Name, "network.incoming.packets.rate")
	th.AssertDeepEquals(t, s.Resource, resources.Resource{
		CreatedByProjectID: "c6b68a6b413648b0a0eb191bf3222f4d",
		CreatedByUserID:    "cb072aacdb494419aeeba5f1c62d1a65",
		Creator:            "cb072aacdb494419aeeba5f1c62d1a65:c6b68a6b413648b0a0eb191bf3222f4d",
		ID:                 "75274f99-faf6-4112-a6d5-2794cb07c789",
		OriginalResourceID: "75274f99-faf6-4112-a6d5-2794cb07c789",
		ProjectID:          "4154f08883334e0494c41155c33c0fc9",
		RevisionStart:      time.Date(2018, 1, 8, 00, 59, 33, 767815000, time.UTC),
		RevisionEnd:        time.Time{},
		StartedAt:          time.Date(2018, 1, 8, 00, 59, 33, 767795000, time.UTC),
		EndedAt:            time.Time{},
		Type:               "compute_instance_network",
		UserID:             "bd5874d666624b24a9f01c128871e4ac",
		ExtraAttributes:    map[string]interface{}{},
	})
	th.AssertEquals(t, s.Unit, "packet/s")
}

func TestCreate(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	th.Mux.HandleFunc("/v1/metric", func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "POST")
		th.TestHeader(t, r, "X-Auth-Token", fake.TokenID)
		th.TestHeader(t, r, "Content-Type", "application/json")
		th.TestHeader(t, r, "Accept", "application/json")
		th.TestJSONRequest(t, r, MetricCreateRequest)

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)

		fmt.Fprintf(w, MetricCreateResponse)
	})

	opts := metrics.CreateOpts{
		ArchivePolicyName: "high",
		Name:              "network.incoming.bytes.rate",
		ResourceID:        "23d5d3f7-9dfa-4f73-b72b-8b0b0063ec55",
		Unit:              "B/s",
	}
	s, err := metrics.Create(fake.ServiceClient(), opts).Extract()
	th.AssertNoErr(t, err)

	th.AssertEquals(t, s.ArchivePolicyName, "high")
	th.AssertEquals(t, s.CreatedByProjectID, "3d40ca37-7234-4911-8987b9f288f4ae84")
	th.AssertEquals(t, s.CreatedByUserID, "fdcfb420-c096-45e6-9e177a0bb1950884")
	th.AssertEquals(t, s.Creator, "fdcfb420-c096-45e6-9e177a0bb1950884:3d40ca37-7234-4911-8987b9f288f4ae84")
	th.AssertEquals(t, s.ID, "01b2953e-de74-448a-a305-c84440697933")
	th.AssertEquals(t, s.Name, "network.incoming.bytes.rate")
	th.AssertEquals(t, s.ResourceID, "23d5d3f7-9dfa-4f73-b72b-8b0b0063ec55")
	th.AssertEquals(t, s.Unit, "B/s")
}

func TestDelete(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	th.Mux.HandleFunc("/v1/metric/01b2953e-de74-448a-a305-c84440697933", func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "DELETE")
		th.TestHeader(t, r, "X-Auth-Token", fake.TokenID)
		w.WriteHeader(http.StatusNoContent)
	})

	res := metrics.Delete(fake.ServiceClient(), "01b2953e-de74-448a-a305-c84440697933")
	th.AssertNoErr(t, res.Err)
}
