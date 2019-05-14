// +build acceptance

package v1

import (
	"bytes"
	"io/ioutil"
	"os"
	"path"
	"testing"

	"github.com/gophercloud/gophercloud/acceptance/tools"
	th "github.com/gophercloud/gophercloud/testhelper"

	"github.com/gophercloud/utils/openstack/clientconfig"
	"github.com/gophercloud/utils/openstack/objectstorage/v1/objects"
)

func TestObjectStreamingUploadDownload(t *testing.T) {
	client, err := clientconfig.NewServiceClient("object-store", nil)
	th.AssertNoErr(t, err)

	// Create a test container to store the object.
	cName, err := CreateContainer(t, client)
	th.AssertNoErr(t, err)
	defer DeleteContainer(t, client, cName)

	// Generate a random object name and random content.
	oName := tools.RandomString("test-object-", 8)
	content := tools.RandomString("", 10)
	contentBuf := bytes.NewBuffer([]byte(content))

	// Upload the object
	uploadOpts := &objects.UploadOpts{
		Content: contentBuf,
	}
	uploadResult, err := objects.Upload(client, cName, oName, uploadOpts)
	defer DeleteObject(t, client, cName, oName)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, uploadResult.Success, true)

	tools.PrintResource(t, uploadResult)

	// Download the object
	downloadOpts := &objects.DownloadOpts{
		OutFile: "-",
	}
	downloadResults, err := objects.Download(client, cName, []string{oName}, downloadOpts)
	th.AssertNoErr(t, err)

	th.AssertEquals(t, len(downloadResults), 1)
	th.AssertEquals(t, downloadResults[0].Success, true)
	downloadedContent, err := ioutil.ReadAll(downloadResults[0].Content)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, string(downloadedContent), content)

	tools.PrintResource(t, downloadResults[0])
}

func TestObjectFileUploadDownload(t *testing.T) {
	client, err := clientconfig.NewServiceClient("object-store", nil)
	th.AssertNoErr(t, err)

	// Create a file with random content
	source, err := CreateRandomFile(t, "/tmp")
	th.AssertNoErr(t, err)
	defer DeleteTempFile(t, source)

	// Create a destination file.
	dest := tools.RandomString("/tmp/test-dest-", 8)

	// Create a random object name.
	oName := tools.RandomString("test-object-", 8)

	// Create a test container to store the object.
	cName, err := CreateContainer(t, client)
	th.AssertNoErr(t, err)
	defer DeleteContainer(t, client, cName)

	// Upload the object
	uploadOpts := &objects.UploadOpts{
		Path: source,
	}

	uploadResult, err := objects.Upload(client, cName, oName, uploadOpts)
	defer DeleteObject(t, client, cName, oName)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, uploadResult.Success, true)

	tools.PrintResource(t, uploadResult)

	// Download the object
	downloadOpts := &objects.DownloadOpts{
		OutFile: dest,
	}
	downloadResults, err := objects.Download(client, cName, []string{oName}, downloadOpts)
	th.AssertNoErr(t, err)
	defer DeleteTempFile(t, dest)

	th.AssertEquals(t, len(downloadResults), 1)
	th.AssertEquals(t, downloadResults[0].Success, true)

	tools.PrintResource(t, downloadResults[0])

	equals, err := CompareFiles(t, source, dest)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, equals, true)
}

func TestObjectStreamingSLO(t *testing.T) {
	client, err := clientconfig.NewServiceClient("object-store", nil)
	th.AssertNoErr(t, err)

	// Create a test container to store the object.
	cName, err := CreateContainer(t, client)
	th.AssertNoErr(t, err)
	defer DeleteContainer(t, client, cName)
	defer DeleteContainer(t, client, cName+"_segments")

	// Generate a random object name and random content.
	oName := tools.RandomString("test-object-", 8)
	content := tools.RandomString("", 256)
	contentBuf := bytes.NewBuffer([]byte(content))

	// Upload the object
	uploadOpts := &objects.UploadOpts{
		Checksum:    true,
		Content:     contentBuf,
		SegmentSize: 62,
		UseSLO:      true,
	}
	uploadResult, err := objects.Upload(client, cName, oName, uploadOpts)
	defer DeleteObject(t, client, cName, oName)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, uploadResult.Success, true)

	tools.PrintResource(t, uploadResult)

	// Download the object
	downloadOpts := &objects.DownloadOpts{
		OutFile: "-",
	}
	downloadResults, err := objects.Download(client, cName, []string{oName}, downloadOpts)
	th.AssertNoErr(t, err)

	th.AssertEquals(t, len(downloadResults), 1)
	th.AssertEquals(t, downloadResults[0].Success, true)

	tools.PrintResource(t, downloadResults[0])

	// Compare the downloaded content with the uploaded.
	downloadedContent, err := ioutil.ReadAll(downloadResults[0].Content)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, string(downloadedContent), content)

	// Replace the object with the same object.
	contentBuf = bytes.NewBuffer([]byte(content))
	uploadOpts.Content = contentBuf
	uploadResult, err = objects.Upload(client, cName, oName, uploadOpts)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, uploadResult.Success, true)

	tools.PrintResource(t, uploadResult)

	// Download the object
	downloadResults, err = objects.Download(client, cName, []string{oName}, downloadOpts)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, len(downloadResults), 1)
	th.AssertEquals(t, downloadResults[0].Success, true)

	tools.PrintResource(t, downloadResults[0])

	// Compare the downloaded content with the uploaded.
	downloadedContent, err = ioutil.ReadAll(downloadResults[0].Content)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, string(downloadedContent), content)
}

func TestObjectFileSLO(t *testing.T) {
	client, err := clientconfig.NewServiceClient("object-store", nil)
	th.AssertNoErr(t, err)

	// Create a file with random content
	source, err := CreateRandomFile(t, "/tmp")
	th.AssertNoErr(t, err)
	defer DeleteTempFile(t, source)

	// Create a destination file.
	dest := tools.RandomString("/tmp/test-dest-", 8)
	defer DeleteTempFile(t, dest)

	// Create a random object name.
	oName := tools.RandomString("test-object-", 8)

	// Create a test container to store the object.
	cName, err := CreateContainer(t, client)
	th.AssertNoErr(t, err)
	defer DeleteContainer(t, client, cName)
	defer DeleteContainer(t, client, cName+"_segments")

	// Upload the object
	uploadOpts := &objects.UploadOpts{
		Path:        source,
		SegmentSize: 62,
		UseSLO:      true,
	}

	uploadResult, err := objects.Upload(client, cName, oName, uploadOpts)
	defer DeleteObject(t, client, cName, oName)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, uploadResult.Success, true)

	tools.PrintResource(t, uploadResult)

	// Download the object
	downloadOpts := &objects.DownloadOpts{
		OutFile: dest,
	}
	downloadResults, err := objects.Download(client, cName, []string{oName}, downloadOpts)
	th.AssertNoErr(t, err)

	th.AssertEquals(t, len(downloadResults), 1)
	th.AssertEquals(t, downloadResults[0].Success, true)

	tools.PrintResource(t, downloadResults[0])

	equals, err := CompareFiles(t, source, dest)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, equals, true)

	tools.PrintResource(t, downloadResults[0])

	// Replace the object with the same object.
	uploadResult, err = objects.Upload(client, cName, oName, uploadOpts)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, uploadResult.Success, true)

	tools.PrintResource(t, uploadResult)

	// Download the object
	downloadResults, err = objects.Download(client, cName, []string{oName}, downloadOpts)
	th.AssertNoErr(t, err)

	th.AssertEquals(t, len(downloadResults), 1)
	th.AssertEquals(t, downloadResults[0].Success, true)

	tools.PrintResource(t, downloadResults[0])

	equals, err = CompareFiles(t, source, dest)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, equals, true)

	tools.PrintResource(t, downloadResults[0])

	// Replace the object with the same object.
	// But skip identical segments
	uploadOpts.SkipIdentical = true
	uploadResult, err = objects.Upload(client, cName, oName, uploadOpts)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, uploadResult.Success, true)
	th.AssertEquals(t, uploadResult.Status, "skip-identical")

	tools.PrintResource(t, uploadResult)

	// Replace the object with the same object.
	// But only if changed.
	uploadOpts.SkipIdentical = false
	uploadOpts.Changed = true
	uploadResult, err = objects.Upload(client, cName, oName, uploadOpts)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, uploadResult.Success, true)
	th.AssertEquals(t, uploadResult.Status, "skip-changed")

	tools.PrintResource(t, uploadResult)
}

func TestObjectFileDLO(t *testing.T) {
	client, err := clientconfig.NewServiceClient("object-store", nil)
	th.AssertNoErr(t, err)

	// Create a file with random content
	source, err := CreateRandomFile(t, "/tmp")
	th.AssertNoErr(t, err)
	defer DeleteTempFile(t, source)

	// Create a destination file.
	dest := tools.RandomString("/tmp/test-dest-", 8)
	defer DeleteTempFile(t, dest)

	// Create a random object name.
	oName := tools.RandomString("test-object-", 8)

	// Create a test container to store the object.
	cName, err := CreateContainer(t, client)
	th.AssertNoErr(t, err)
	defer DeleteContainer(t, client, cName)
	defer DeleteContainer(t, client, cName+"_segments")

	// Upload the object
	uploadOpts := &objects.UploadOpts{
		Checksum:    true,
		Path:        source,
		SegmentSize: 62,
	}

	uploadResult, err := objects.Upload(client, cName, oName, uploadOpts)
	defer DeleteObject(t, client, cName, oName)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, uploadResult.Success, true)

	tools.PrintResource(t, uploadResult)

	// Download the object
	downloadOpts := &objects.DownloadOpts{
		OutFile: dest,
	}
	downloadResults, err := objects.Download(client, cName, []string{oName}, downloadOpts)
	th.AssertNoErr(t, err)

	th.AssertEquals(t, len(downloadResults), 1)
	th.AssertEquals(t, downloadResults[0].Success, true)

	tools.PrintResource(t, downloadResults[0])

	equals, err := CompareFiles(t, source, dest)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, equals, true)

	// Replace the object with the same object.
	uploadResult, err = objects.Upload(client, cName, oName, uploadOpts)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, uploadResult.Success, true)

	tools.PrintResource(t, uploadResult)

	// Download the object
	downloadResults, err = objects.Download(client, cName, []string{oName}, downloadOpts)
	th.AssertNoErr(t, err)

	th.AssertEquals(t, len(downloadResults), 1)
	th.AssertEquals(t, downloadResults[0].Success, true)

	tools.PrintResource(t, downloadResults[0])

	equals, err = CompareFiles(t, source, dest)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, equals, true)

	tools.PrintResource(t, downloadResults[0])

	// Replace the object with the same object.
	// But skip identical segments
	uploadOpts.SkipIdentical = true
	uploadResult, err = objects.Upload(client, cName, oName, uploadOpts)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, uploadResult.Success, true)
	th.AssertEquals(t, uploadResult.Status, "skip-identical")

	tools.PrintResource(t, uploadResult)

	// Replace the object with the same object.
	// But only if changed.
	uploadOpts.SkipIdentical = false
	uploadOpts.Changed = true
	uploadResult, err = objects.Upload(client, cName, oName, uploadOpts)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, uploadResult.Success, true)
	th.AssertEquals(t, uploadResult.Status, "skip-changed")

	tools.PrintResource(t, uploadResult)
}

func TestObjectPseudoDirBasic(t *testing.T) {
	client, err := clientconfig.NewServiceClient("object-store", nil)
	th.AssertNoErr(t, err)

	// Create a test container to store the object.
	cName, err := CreateContainer(t, client)
	th.AssertNoErr(t, err)
	defer DeleteContainer(t, client, cName)

	// Generate a random object name and random content.
	oName := tools.RandomString("test-object-", 8)

	// Create the directory marker
	uploadOpts := &objects.UploadOpts{
		DirMarker: true,
	}
	uploadResult, err := objects.Upload(client, cName, oName, uploadOpts)
	defer DeleteObject(t, client, cName, oName)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, uploadResult.Success, true)

	tools.PrintResource(t, uploadResult)

	// Get the object
	obj, err := GetObject(client, cName, oName)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, obj.ContentType, "application/directory")
}

func TestObjectPseudoDirFileStructure(t *testing.T) {
	client, err := clientconfig.NewServiceClient("object-store", nil)
	th.AssertNoErr(t, err)

	// Create a test container to store the object.
	cName, err := CreateContainer(t, client)
	th.AssertNoErr(t, err)
	defer DeleteContainer(t, client, cName)

	// Create a temporary directory to hold files.
	parentDir, err := CreateTempDir(t, "/tmp")
	th.AssertNoErr(t, err)
	defer DeleteTempDir(t, parentDir)

	oName := path.Base(parentDir)

	// Upload the directory
	uploadOpts := &objects.UploadOpts{
		Path: parentDir,
	}
	uploadResult, err := objects.Upload(client, cName, oName, uploadOpts)
	defer DeleteObject(t, client, cName, oName)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, uploadResult.Success, true)

	tools.PrintResource(t, uploadResult)

	// Create a file with random content
	source, err := CreateRandomFile(t, parentDir)
	th.AssertNoErr(t, err)
	defer DeleteTempFile(t, source)

	oName = path.Join(oName, path.Base(source))

	// Upload the file.
	uploadOpts.Path = source
	uploadResult, err = objects.Upload(client, cName, oName, uploadOpts)
	defer DeleteObject(t, client, cName, oName)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, uploadResult.Success, true)

	tools.PrintResource(t, uploadResult)

	// Create a nested directory to hold files.
	nestedDir, err := CreateTempDir(t, parentDir)
	th.AssertNoErr(t, err)
	defer DeleteTempDir(t, nestedDir)

	oName = path.Join(path.Base(parentDir), path.Base(nestedDir))

	// Upload the nested directory
	uploadOpts.Path = nestedDir
	uploadResult, err = objects.Upload(client, cName, oName, uploadOpts)
	defer DeleteObject(t, client, cName, oName)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, uploadResult.Success, true)

	tools.PrintResource(t, uploadResult)

	// Create a file in the nested directory with random content
	nestedSource, err := CreateRandomFile(t, nestedDir)
	th.AssertNoErr(t, err)
	defer DeleteTempFile(t, nestedSource)

	oName = path.Join(oName, path.Base(nestedSource))

	// Upload the file.
	uploadOpts.Path = nestedSource
	uploadResult, err = objects.Upload(client, cName, oName, uploadOpts)
	defer DeleteObject(t, client, cName, oName)
	th.AssertNoErr(t, err)
	th.AssertEquals(t, uploadResult.Success, true)

	tools.PrintResource(t, uploadResult)

	// Create a temporary directory to download files.
	downloadDir, err := CreateTempDir(t, "/tmp")
	th.AssertNoErr(t, err)
	defer DeleteTempDir(t, downloadDir)

	// Download the container to downloadDir
	downloadOpts := &objects.DownloadOpts{
		OutDirectory: downloadDir,
	}
	downloadResults, err := objects.Download(client, cName, []string{}, downloadOpts)
	th.AssertNoErr(t, err)

	// Compare the downloaded content
	for _, dr := range downloadResults {
		pseudoDir := dr.PseudoDir
		stat, err := os.Stat(dr.Path)
		th.AssertNoErr(t, err)
		th.AssertEquals(t, stat.IsDir(), pseudoDir)

		if !pseudoDir {
			v := path.Join("/tmp", dr.Object)
			equals, err := CompareFiles(t, v, dr.Path)
			th.AssertNoErr(t, err)
			th.AssertEquals(t, equals, true)
		}
	}

	tools.PrintResource(t, downloadResults)
}
