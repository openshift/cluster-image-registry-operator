package azureclient

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	azfake "github.com/Azure/azure-sdk-for-go/sdk/azcore/fake"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v4"
	networkfake "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v4/fake"
	autorestazure "github.com/Azure/go-autorest/autorest/azure"
	"github.com/Azure/go-autorest/autorest/to"
)

type testDoer struct {
	response []*http.Response
	body     string
}

// Do implements the Doer interface for mocking.
// Do accepts the passed request and body, then appends the response and emits it.
func (td *testDoer) Do(r *http.Request) (*http.Response, error) {
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Request:    r,
		Body:       io.NopCloser(bytes.NewBufferString(td.body)),
	}
	td.response = append(td.response, resp)
	return resp, nil
}

func TestNew(t *testing.T) {
	t.Run("with empty options", func(t *testing.T) {
		_, err := New(&Options{})
		if err == nil {
			t.Fatal("new with no options should fail, but error was nil")
		}
		msg := "client misconfigured, missing 'Environment.ResourceManagerEndpoint', 'Environment.ActiveDirectoryEndpoint', 'Environment.TokenAudience', 'TenantID', 'ClientID', 'ClientSecret', 'FederatedTokenFile', 'Creds', 'SubscriptionID' option(s)"
		if err.Error() != msg {
			t.Error("client failed with wrong error")
			t.Logf("want %q", msg)
			t.Logf("got %q", err)
		}
	})
	t.Run("with correct options", func(t *testing.T) {
		opts := &Options{
			Environment: autorestazure.Environment{
				ActiveDirectoryEndpoint: "https://test-active-directory-endpoint",
				TokenAudience:           "test-token-audience",
				ResourceManagerEndpoint: "test-resource-manager-endpoint",
			},
			TenantID:       "test-tenant-id",
			ClientID:       "test-client-id",
			ClientSecret:   "test-client-secret",
			SubscriptionID: "test-subscription-id",
		}
		_, err := New(opts)
		if err != nil {
			t.Errorf("unexpected error: %s", err)
		}
	})
}

func TestPrivateEndpointWithTagExists(t *testing.T) {
	tagKey := "test-tagkey"
	tagValue := "test-tagvalue"
	ctx := context.Background()
	fakeSvr := networkfake.PrivateEndpointsServer{}
	fakeSvr.NewListPager = func(
		resourceGroupName string,
		options *armnetwork.PrivateEndpointsClientListOptions,
	) (resp azfake.PagerResponder[armnetwork.PrivateEndpointsClientListResponse]) {
		peResp := armnetwork.PrivateEndpointsClientListResponse{
			PrivateEndpointListResult: armnetwork.PrivateEndpointListResult{
				Value: []*armnetwork.PrivateEndpoint{{
					Tags: map[string]*string{
						"foo":   to.StringPtr("bar"),
						tagKey:  to.StringPtr(tagValue),
						"test1": to.StringPtr("testvalue1"),
					},
				}},
			},
		}
		resp.AddPage(http.StatusOK, peResp, nil)
		return resp
	}
	fakeTransport := networkfake.NewPrivateEndpointsServerTransport(&fakeSvr)
	client, err := New(&Options{
		Environment: autorestazure.Environment{
			ActiveDirectoryEndpoint: "https://test-active-directory-endpoint",
			TokenAudience:           "test-token-audience",
			ResourceManagerEndpoint: "https://test-resource-manager-endpoint",
		},
		TenantID:       "test-tenant-id",
		ClientID:       "test-client-id",
		ClientSecret:   "test-client-secret",
		SubscriptionID: "test-subscription-id",
		Creds:          &azfake.TokenCredential{},
		HTTPClient:     fakeTransport,
	})
	if err != nil {
		t.Errorf("unexpected error: %q", err)
	}
	_, exists := client.PrivateEndpointWithTagExists(ctx, "resource-group", tagKey, tagValue)
	if !exists {
		t.Fatalf("expected private endpoint with tag key %q and value %q to exist, but it didn't", tagKey, tagValue)
	}

	// now attempt to get a tag that the endpoint does not have and assert
	// the function returns false
	_, exists = client.PrivateEndpointWithTagExists(ctx, "resource-group", "tag-key-not-there", "no-tag-with-this-value")
	if exists {
		t.Fatal("expected private endpoint to not exist, but it did")
	}
}

func TestPrivateEndpointExists(t *testing.T) {
	ctx := context.Background()

	client, err := New(&Options{
		Environment: autorestazure.Environment{
			ActiveDirectoryEndpoint: "https://test-active-directory-endpoint",
			TokenAudience:           "test-token-audience",
			ResourceManagerEndpoint: "https://test-resource-manager-endpoint",
		},
		TenantID:       "adfs",
		ClientID:       "test-client-id",
		ClientSecret:   "test-client-secret",
		SubscriptionID: "test-subscription-id",
		HTTPClient:     &testDoer{},
		Creds:          &azfake.TokenCredential{},
	})
	if err != nil {
		t.Fatalf("failed to create client: %q", err)
	}
	exists, err := client.PrivateEndpointExists(ctx, "test-resource-group", "my-private-endpoint")
	if err != nil {
		t.Fatalf("failed to check if private endpoint exists: %q", err)
	}
	if !exists {
		t.Fatal("expected private endpoint to exist, but it didn't")
	}
}

func TestCreatePrivateEndpoint(t *testing.T) {
	ctx := context.Background()
	accountName := "imageregistry-abc123"
	createOpts := &PrivateEndpointCreateOptions{
		Location:                 "global",
		NetworkResourceGroupName: "my-rg-2",
		VNetName:                 "ocp-cluster-vnet",
		SubnetName:               "worker-subnet",
		PrivateEndpointName:      "imageregistry-abc123",
		StorageAccountName:       accountName,
		ClusterResourceGroupName: "my-rg-1",
	}
	client, err := New(&Options{
		Environment: autorestazure.Environment{
			ActiveDirectoryEndpoint: "https://test-active-directory-endpoint",
			TokenAudience:           "test-token-audience",
			ResourceManagerEndpoint: "https://test-resource-manager-endpoint",
		},
		TenantID:       "test-tenant-id",
		ClientID:       "test-client-id",
		ClientSecret:   "test-client-secret",
		SubscriptionID: "test-subscription-id",
		HTTPClient:     &testDoer{},
		Creds:          azfake.NewTokenCredential(),
	})
	if err != nil {
		t.Errorf("unexpected error: %q", err)
	}
	_, err = client.CreatePrivateEndpoint(ctx, createOpts)
	if err != nil {
		t.Fatalf("unexpected error: %q", err)
	}
}
