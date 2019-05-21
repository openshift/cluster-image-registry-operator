package testing

import (
	"testing"

	o "github.com/gophercloud/gophercloud/openstack/objectstorage/v1/objects"
	th "github.com/gophercloud/gophercloud/testhelper"
	fake "github.com/gophercloud/gophercloud/testhelper/client"
	"github.com/gophercloud/utils/openstack/objectstorage/v1/objects"
)

func TestIsIdentical(t *testing.T) {
	cd := []objects.Manifest{
		{
			Bytes: 2,
			Hash:  "60b725f10c9c85c70d97880dfe8191b3",
		},
		{
			Bytes: 2,
			Hash:  "3b5d5c3712955042212316173ccf37be",
		},
		{
			Bytes: 2,
			Hash:  "2cd6ee2c70b0bde53fbe6cac3c8b8bb1",
		},
		{
			Bytes: 2,
			Hash:  "e29311f6f1bf1af907f9ef9f44b8328b",
		},
	}

	actual, err := objects.IsIdentical(cd, "t.txt")
	th.AssertNoErr(t, err)
	th.AssertEquals(t, true, actual)
}

func TestMultipartManifest(t *testing.T) {
	actual, err := objects.ExtractMultipartManifest([]byte(multipartManifest))
	th.AssertNoErr(t, err)
	th.AssertDeepEquals(t, expectedMultipartManifest, actual)
}

func TestChunkData(t *testing.T) {
	th.SetupHTTP()
	defer th.TeardownHTTP()
	HandleDownloadManifestSuccessfully(t)

	downloadOpts := o.DownloadOpts{
		MultipartManifest: "get",
	}

	res := o.Download(fake.ServiceClient(), "testContainer", "testObject", downloadOpts)
	defer res.Body.Close()
	th.AssertNoErr(t, res.Err)

	body, err := res.ExtractContent()
	th.AssertNoErr(t, err)

	actualMultipartManifest, err := objects.ExtractMultipartManifest(body)
	th.AssertNoErr(t, err)
	th.AssertDeepEquals(t, actualMultipartManifest, expectedMultipartManifest)

	gmo := objects.GetManifestOpts{
		ContainerName:     "testContainer",
		ObjectName:        "testObject",
		StaticLargeObject: true,
	}

	actualChunkData, err := objects.GetManifest(fake.ServiceClient(), gmo)
	th.AssertNoErr(t, err)
	th.AssertDeepEquals(t, actualChunkData, expectedMultipartManifest)
}
