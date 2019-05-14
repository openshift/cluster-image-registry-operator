package testing

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/gophercloud/gophercloud/pagination"
	th "github.com/gophercloud/gophercloud/testhelper"
	"github.com/gophercloud/utils/gnocchi/metric/v1/archivepolicies"
	fake "github.com/gophercloud/utils/gnocchi/testhelper/client"
)

func TestListArchivePolicies(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	th.Mux.HandleFunc("/v1/archive_policy", func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "GET")
		th.TestHeader(t, r, "X-Auth-Token", fake.TokenID)

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		fmt.Fprintf(w, ArchivePoliciesListResult)
	})

	expected := ListArchivePoliciesExpected
	pages := 0
	err := archivepolicies.List(fake.ServiceClient()).EachPage(func(page pagination.Page) (bool, error) {
		pages++

		actual, err := archivepolicies.ExtractArchivePolicies(page)
		th.AssertNoErr(t, err)

		if len(actual) != 2 {
			t.Fatalf("Expected 2 archive policy, got %d", len(actual))
		}
		th.CheckDeepEquals(t, expected, actual)

		return true, nil
	})
	th.AssertNoErr(t, err)
	th.CheckEquals(t, 1, pages)
}

func TestListArchivePoliciesAllPages(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	th.Mux.HandleFunc("/v1/archive_policy", func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "GET")
		th.TestHeader(t, r, "X-Auth-Token", fake.TokenID)

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		fmt.Fprintf(w, ArchivePoliciesListResult)
	})

	allPages, err := archivepolicies.List(fake.ServiceClient()).AllPages()
	th.AssertNoErr(t, err)
	_, err = archivepolicies.ExtractArchivePolicies(allPages)
	th.AssertNoErr(t, err)
}

func TestGetArchivePolicy(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	th.Mux.HandleFunc("/v1/archive_policy/test_policy", func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "GET")
		th.TestHeader(t, r, "X-Auth-Token", fake.TokenID)

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		fmt.Fprintf(w, ArchivePolicyGetResult)
	})

	s, err := archivepolicies.Get(fake.ServiceClient(), "test_policy").Extract()
	th.AssertNoErr(t, err)

	th.AssertDeepEquals(t, s.AggregationMethods, []string{
		"max",
		"min",
		"mean",
	})
	th.AssertEquals(t, s.BackWindow, 128)
	th.AssertDeepEquals(t, s.Definition, []archivepolicies.ArchivePolicyDefinition{
		{
			Granularity: "1:00:00",
			Points:      2160,
			TimeSpan:    "90 days, 0:00:00",
		},
		{
			Granularity: "1 day, 0:00:00",
			Points:      100,
			TimeSpan:    "100 days, 0:00:00",
		},
	})
	th.AssertEquals(t, s.Name, "test_policy")
}

func TestCreate(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	th.Mux.HandleFunc("/v1/archive_policy", func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "POST")
		th.TestHeader(t, r, "X-Auth-Token", fake.TokenID)
		th.TestHeader(t, r, "Content-Type", "application/json")
		th.TestHeader(t, r, "Accept", "application/json")
		th.TestJSONRequest(t, r, ArchivePolicyCreateRequest)

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)

		fmt.Fprintf(w, ArchivePolicyCreateResponse)
	})

	opts := archivepolicies.CreateOpts{
		BackWindow: 31,
		AggregationMethods: []string{
			"sum",
			"mean",
			"count",
		},
		Definition: []archivepolicies.ArchivePolicyDefinitionOpts{
			{
				Granularity: "1:00:00",
				TimeSpan:    "90 days, 0:00:00",
			},
			{
				Granularity: "1 day, 0:00:00",
				TimeSpan:    "100 days, 0:00:00",
			},
		},
		Name: "test_policy",
	}
	s, err := archivepolicies.Create(fake.ServiceClient(), opts).Extract()
	th.AssertNoErr(t, err)

	th.AssertDeepEquals(t, s.AggregationMethods, []string{
		"sum",
		"mean",
		"count",
	})
	th.AssertEquals(t, s.BackWindow, 31)
	th.AssertDeepEquals(t, s.Definition, []archivepolicies.ArchivePolicyDefinition{
		{
			Granularity: "1:00:00",
			Points:      2160,
			TimeSpan:    "90 days, 0:00:00",
		},
		{
			Granularity: "1 day, 0:00:00",
			Points:      100,
			TimeSpan:    "100 days, 0:00:00",
		},
	})
	th.AssertEquals(t, s.Name, "test_policy")
}

func TestUpdateArchivePolicy(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	th.Mux.HandleFunc("/v1/archive_policy/test_policy", func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "PATCH")
		th.TestHeader(t, r, "X-Auth-Token", fake.TokenID)
		th.TestHeader(t, r, "Content-Type", "application/json")
		th.TestHeader(t, r, "Accept", "application/json")
		th.TestJSONRequest(t, r, ArchivePolicyUpdateRequest)

		w.Header().Add("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		fmt.Fprintf(w, ArchivePolicyUpdateResponse)
	})

	updateOpts := archivepolicies.UpdateOpts{
		Definition: []archivepolicies.ArchivePolicyDefinitionOpts{
			{
				Granularity: "12:00:00",
				TimeSpan:    "30 days, 0:00:00",
			},
			{
				Granularity: "1 day, 0:00:00",
				TimeSpan:    "90 days, 0:00:00",
			},
		},
	}
	s, err := archivepolicies.Update(fake.ServiceClient(), "test_policy", updateOpts).Extract()
	th.AssertNoErr(t, err)

	th.AssertDeepEquals(t, s.AggregationMethods, []string{
		"max",
	})
	th.AssertEquals(t, s.BackWindow, 0)
	th.AssertDeepEquals(t, s.Definition, []archivepolicies.ArchivePolicyDefinition{
		{
			Granularity: "12:00:00",
			Points:      60,
			TimeSpan:    "30 days, 0:00:00",
		},
		{
			Granularity: "1 day, 0:00:00",
			Points:      90,
			TimeSpan:    "90 days, 0:00:00",
		},
	})
	th.AssertEquals(t, s.Name, "test_policy")
}

func TestDelete(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()

	th.Mux.HandleFunc("/v1/archive_policy/test_policy", func(w http.ResponseWriter, r *http.Request) {
		th.TestMethod(t, r, "DELETE")
		th.TestHeader(t, r, "X-Auth-Token", fake.TokenID)
		w.WriteHeader(http.StatusNoContent)
	})

	res := archivepolicies.Delete(fake.ServiceClient(), "test_policy")
	th.AssertNoErr(t, res.Err)
}
