// +build acceptance metric resourcetypes

package v1

import (
	"testing"

	"github.com/gophercloud/gophercloud/acceptance/tools"
	"github.com/gophercloud/utils/acceptance/clients"
	"github.com/gophercloud/utils/gnocchi/metric/v1/resourcetypes"
)

func TestResourceTypesList(t *testing.T) {
	client, err := clients.NewGnocchiV1Client()
	if err != nil {
		t.Fatalf("Unable to create a Gnocchi client: %v", err)
	}

	allPages, err := resourcetypes.List(client).AllPages()
	if err != nil {
		t.Fatalf("Unable to list resource types: %v", err)
	}

	allResourceTypes, err := resourcetypes.ExtractResourceTypes(allPages)
	if err != nil {
		t.Fatalf("Unable to extract resource types: %v", err)
	}

	for _, resourceType := range allResourceTypes {
		tools.PrintResource(t, resourceType)
	}
}

func TestResourceTypesCRUD(t *testing.T) {
	client, err := clients.NewGnocchiV1Client()
	if err != nil {
		t.Fatalf("Unable to create a Gnocchi client: %v", err)
	}

	resourceType, err := CreateResourceType(t, client)
	if err != nil {
		t.Fatalf("Unable to create a Gnocchi resource type: %v", err)
	}
	defer DeleteResourceType(t, client, resourceType.Name)

	tools.PrintResource(t, resourceType)

	// Populate attributes that will be deleted.
	attributesToDelete := []string{}
	for attributeName := range resourceType.Attributes {
		attributesToDelete = append(attributesToDelete, attributeName)
	}

	// New string attribute parameters.
	newStringAttributeOpts := resourcetypes.AttributeOpts{
		Details: map[string]interface{}{
			"required":   false,
			"min_length": 32,
			"max_length": 64,
		},
		Type: "string",
	}
	newStringAttributeOptsName := tools.RandomString("TESTACCT-ATTRIBUTE-", 8)

	// New datetime attribute parameters.
	newDatetimeAttributeOpts := resourcetypes.AttributeOpts{
		Details: map[string]interface{}{
			"required": true,
			"options": map[string]interface{}{
				"fill": "2018-07-28T00:01:01Z",
			},
		},
		Type: "datetime",
	}
	newDatetimeAttributeOptsName := tools.RandomString("TESTACCT-ATTRIBUTE-", 8)

	// Initial options for the Update request with the new resource type attributes.
	updateOpts := resourcetypes.UpdateOpts{
		Attributes: []resourcetypes.AttributeUpdateOpts{
			{
				Name:      newStringAttributeOptsName,
				Operation: resourcetypes.AttributeAdd,
				Value:     &newStringAttributeOpts,
			},
			{
				Name:      newDatetimeAttributeOptsName,
				Operation: resourcetypes.AttributeAdd,
				Value:     &newDatetimeAttributeOpts,
			},
		},
	}

	// Add options to delete attributes.
	for _, attributeToDelete := range attributesToDelete {
		updateOpts.Attributes = append(updateOpts.Attributes, resourcetypes.AttributeUpdateOpts{
			Name:      attributeToDelete,
			Operation: resourcetypes.AttributeRemove,
		})
	}

	t.Logf("Attempting to update a Gnocchi resource type \"%s\".", resourceType.Name)
	newResourceType, err := resourcetypes.Update(client, resourceType.Name, updateOpts).Extract()
	if err != nil {
		t.Fatalf("Unable to update a Gnocchi resource type: %v", err)
	}
	t.Logf("Successfully updated the Gnocchi resource type \"%s\".", resourceType.Name)

	tools.PrintResource(t, newResourceType)
}
