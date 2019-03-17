// +build acceptance metric archivepolicies

package v1

import (
	"testing"

	"github.com/gophercloud/gophercloud/acceptance/tools"
	"github.com/gophercloud/utils/acceptance/clients"
	"github.com/gophercloud/utils/gnocchi/metric/v1/archivepolicies"
)

func TestArchivePoliciesCRUD(t *testing.T) {
	client, err := clients.NewGnocchiV1Client()
	if err != nil {
		t.Fatalf("Unable to create a Gnocchi client: %v", err)
	}

	archivePolicy, err := CreateArchivePolicy(t, client)
	if err != nil {
		t.Fatalf("Unable to create a Gnocchi archive policy: %v", err)
	}
	defer DeleteArchivePolicy(t, client, archivePolicy.Name)

	tools.PrintResource(t, archivePolicy)

	updateOpts := archivepolicies.UpdateOpts{
		Definition: []archivepolicies.ArchivePolicyDefinitionOpts{
			{
				Granularity: "1:00:00",
				TimeSpan:    "90 days, 0:00:00",
			},
			{
				Granularity: "24:00:00",
				TimeSpan:    "365 days, 0:00:00",
			},
		},
	}
	t.Logf("Attempting to update an archive policy %s", archivePolicy.Name)
	newArchivePolicy, err := archivepolicies.Update(client, archivePolicy.Name, updateOpts).Extract()
	if err != nil {
		t.Fatalf("Unable to update a Gnocchi archive policy: %v", err)
	}

	tools.PrintResource(t, newArchivePolicy)
}

func TestArchivePoliciesList(t *testing.T) {
	client, err := clients.NewGnocchiV1Client()
	if err != nil {
		t.Fatalf("Unable to create a Gnocchi client: %v", err)
	}

	allPages, err := archivepolicies.List(client).AllPages()
	if err != nil {
		t.Fatalf("Unable to list archive policies: %v", err)
	}

	allArchivePolicies, err := archivepolicies.ExtractArchivePolicies(allPages)
	if err != nil {
		t.Fatalf("Unable to extract archive policies: %v", err)
	}

	for _, archivePolicy := range allArchivePolicies {
		tools.PrintResource(t, archivePolicy)
	}
}
