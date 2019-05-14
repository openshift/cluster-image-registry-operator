package testing

import (
	"testing"

	th "github.com/gophercloud/gophercloud/testhelper"
	"github.com/gophercloud/utils/openstack/objectstorage/v1/objects"
)

func TestContainerPartition(t *testing.T) {
	containerName := "foo/bar/baz"

	expectedContainerName := "foo"
	expectedPseudoFolder := "bar/baz"

	actualContainerName, actualPseudoFolder := objects.ContainerPartition(containerName)
	th.AssertEquals(t, expectedContainerName, actualContainerName)
	th.AssertEquals(t, expectedPseudoFolder, actualPseudoFolder)

	containerName = "foo"
	expectedContainerName = "foo"
	expectedPseudoFolder = ""

	actualContainerName, actualPseudoFolder = objects.ContainerPartition(containerName)
	th.AssertEquals(t, expectedContainerName, actualContainerName)
	th.AssertEquals(t, expectedPseudoFolder, actualPseudoFolder)
}
