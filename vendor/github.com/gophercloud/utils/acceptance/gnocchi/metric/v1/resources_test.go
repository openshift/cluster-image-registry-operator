// +build acceptance metric resources

package v1

import (
	"testing"
	"time"

	"github.com/gophercloud/gophercloud/acceptance/tools"
	"github.com/gophercloud/utils/acceptance/clients"
	"github.com/gophercloud/utils/gnocchi/metric/v1/resources"
)

func TestResourcesCRUD(t *testing.T) {
	client, err := clients.NewGnocchiV1Client()
	if err != nil {
		t.Fatalf("Unable to create a Gnocchi client: %v", err)
	}

	genericResource, err := CreateGenericResource(t, client)
	if err != nil {
		t.Fatalf("Unable to create a generic Gnocchi resource: %v", err)
	}
	defer DeleteResource(t, client, genericResource.Type, genericResource.ID)

	tools.PrintResource(t, genericResource)

	newStartedAt := time.Date(2018, 1, 1, 1, 1, 0, 0, time.UTC)
	newMetrics := map[string]interface{}{}
	updateOpts := &resources.UpdateOpts{
		StartedAt: &newStartedAt,
		Metrics:   &newMetrics,
	}
	t.Logf("Attempting to update a resource %s", genericResource.ID)
	newGenericResource, err := resources.Update(client, genericResource.Type, genericResource.ID, updateOpts).Extract()
	if err != nil {
		t.Fatalf("Unable to update the generic Gnocchi resource: %v", err)
	}

	tools.PrintResource(t, newGenericResource)
}

func TestResourcesList(t *testing.T) {
	client, err := clients.NewGnocchiV1Client()
	if err != nil {
		t.Fatalf("Unable to create a Gnocchi client: %v", err)
	}

	opts := resources.ListOpts{}
	resourceType := "generic"
	allPages, err := resources.List(client, opts, resourceType).AllPages()
	if err != nil {
		t.Fatalf("Unable to list resources: %v", err)
	}

	allResources, err := resources.ExtractResources(allPages)
	if err != nil {
		t.Fatalf("Unable to extract resources: %v", err)
	}

	for _, resource := range allResources {
		tools.PrintResource(t, resource)
	}
}
