package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
)

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func TestMoveBlobs(t *testing.T) {
	ctx := context.Background()
	opts := getConfigOpts()
	storageAccountURL := fmt.Sprintf("https://%s.blob.core.windows.net/", opts.storageAccountName)
	accountKey := os.Getenv("AZURE_ACCOUNTKEY")

	blobsWithLeadingSlash := map[string]string{
		"/docker/registry/v2/blobs/sha256/1c/1c1f781955aa0dcf0e54d83fd8b5e757915a85676a40f9b169cf4fd2c5e92b68/data": randStringRunes(4 * 1024 * 1024),
		"/docker/registry/v2/blobs/sha256/39/393be486280f2dca8858178237fb1918bfa05b6d62386647b51067f128251d4f/data": randStringRunes(4 * 1024 * 1024),
		"/docker/registry/v2/blobs/sha256/b1/b195d8055f37a88a080652c5008e192d5525c2d5b1c987f5987c9c9bfd12e771/data": randStringRunes(4 * 1024 * 1024),
	}
	blobsWithoutLeadingSlash := map[string]string{
		"docker/registry/v2/blobs/sha256/18/18ca996a454fc86375a6ea7ad01157a6b39e28c32460d36eb1479d42334e57ad/data": randStringRunes(4 * 1024 * 1024),
		"docker/registry/v2/blobs/sha256/72/72c9e456423548988a55fa920bb35c194d568ca1959ffcc7316c02e2f60ea0ff/data": randStringRunes(4 * 1024 * 1024),
		"docker/registry/v2/blobs/sha256/fa/fa0975ea1d6b889364d8ae38f84df5e2a8630e26fe49e999aa47e7a4c1b7ee33/data": randStringRunes(4 * 1024 * 1024),
	}

	cred, err := azblob.NewSharedKeyCredential(opts.storageAccountName, accountKey)
	if err != nil {
		t.Fatal(err)
	}
	client, err := azblob.NewClientWithSharedKeyCredential(storageAccountURL, cred, nil)
	if err != nil {
		t.Fatal(err)
	}

	for blobName, blobData := range blobsWithLeadingSlash {
		_, err := client.UploadStream(ctx, opts.containerName, blobName, strings.NewReader(blobData), nil)
		if err != nil {
			t.Fatalf("failed to upload blob: %q", err)
		}
		t.Logf("uploaded blob %q...", blobName)

		defer client.DeleteBlob(ctx, opts.containerName, blobName, nil)
	}
	blobNamesWithoutLeadingSlash := []string{} // we'll use this later for assertions
	for blobName, blobData := range blobsWithoutLeadingSlash {
		_, err := client.UploadStream(ctx, opts.containerName, blobName, strings.NewReader(blobData), nil)
		if err != nil {
			t.Fatalf("failed to upload blob: %q", err)
		}
		t.Logf("uploaded blob %q...", blobName)

		blobNamesWithoutLeadingSlash = append(blobNamesWithoutLeadingSlash, blobName)

		// this call in only necessary if moveBlobs fails to delete a copied blob
		defer client.DeleteBlob(ctx, opts.containerName, blobName, nil)
		// since these blobs will be copied to a location with a leading slash, we delete
		// them from there as well.
		defer client.DeleteBlob(ctx, opts.containerName, "/"+blobName, nil)
	}

	cloudConfig, err := getCloudConfig(opts.environment)
	if err != nil {
		t.Fatal(err)
	}
	containerClient, err := getClient(cloudConfig, opts)
	if err != nil {
		t.Fatal(err)
	}

	movedBlobs, err := moveBlobs(
		ctx,
		containerClient,
		&moveBlobOpts{
			source: "docker",
			dest:   "/docker",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	// check that movedBlobs is an exact match to blobNamesWithoutLeadingSlash
	if len(blobNamesWithoutLeadingSlash) != len(movedBlobs) {
		t.Errorf(
			"list of moved blobs and list of blobs without slash lengths didn't match: want %d but got %d",
			len(blobNamesWithoutLeadingSlash),
			len(movedBlobs),
		)
	}
	for _, blobNameWithoutLeadingSlash := range blobNamesWithoutLeadingSlash {
		moved := false
		for _, movedBlob := range movedBlobs {
			if movedBlob == blobNameWithoutLeadingSlash {
				moved = true
				break
			}
		}
		if !moved {
			t.Logf("blob was not present in movedBlobs list: %q", blobNameWithoutLeadingSlash)
		}
	}

	// check that moved blobs exist in destination dir,
	// then check that the source blobs were actually removed.
	for _, blobName := range movedBlobs {
		newBlobName := "/" + blobName
		blobClient := containerClient.NewBlobClient(newBlobName)

		_, err := blobClient.GetProperties(ctx, nil)
		is404 := bloberror.HasCode(err, bloberror.BlobNotFound)
		if err != nil {
			errMsg := fmt.Sprintf("blob did not exist in destination: %q", newBlobName)
			if !is404 {
				errMsg = fmt.Sprintf("failed to get blob properties: %v", err)
			}
			t.Error(errMsg)
		}

		blobClient = containerClient.NewBlobClient(blobName)
		_, err = blobClient.GetProperties(ctx, nil)
		is404 = bloberror.HasCode(err, bloberror.BlobNotFound)
		if err == nil || !is404 {
			t.Errorf("expected not found error, got: %v", err)
		}
	}
}

func TestValidation(t *testing.T) {
	testCases := []struct {
		name        string
		expectError bool
		opts        *configOpts
	}{
		{
			name:        "valid with federated token file",
			expectError: false,
			opts: &configOpts{
				storageAccountName: "teststorageaccount",
				containerName:      "test-container",
				clientID:           "test-client-id",
				tenantID:           "test-tenant-id",
				clientSecret:       "",
				federatedTokenFile: "federated-token-file",
			},
		},
		{
			name:        "valid with client secret",
			expectError: false,
			opts: &configOpts{
				storageAccountName: "teststorageaccount",
				containerName:      "test-container",
				clientID:           "test-client-id",
				tenantID:           "test-tenant-id",
				clientSecret:       "client-secret",
				federatedTokenFile: "",
			},
		},
		{
			name:        "invalid: no client secret or federated token file",
			expectError: true,
			opts: &configOpts{
				storageAccountName: "teststorageaccount",
				containerName:      "test-container",
				clientID:           "test-client-id",
				tenantID:           "test-tenant-id",
				clientSecret:       "",
				federatedTokenFile: "",
			},
		},
		{
			name:        "invalid: no storage account name",
			expectError: true,
			opts: &configOpts{
				storageAccountName: "",
				containerName:      "test-container",
				clientID:           "test-client-id",
				tenantID:           "test-tenant-id",
				clientSecret:       "client-secret",
				federatedTokenFile: "",
			},
		},
		{
			name:        "invalid: no container name",
			expectError: true,
			opts: &configOpts{
				storageAccountName: "storageaccountname",
				containerName:      "",
				clientID:           "test-client-id",
				tenantID:           "test-tenant-id",
				clientSecret:       "client-secret",
				federatedTokenFile: "",
			},
		},
		{
			name:        "invalid: no client id",
			expectError: true,
			opts: &configOpts{
				storageAccountName: "storageaccountname",
				containerName:      "test-container",
				clientID:           "",
				tenantID:           "test-tenant-id",
				clientSecret:       "client-secret",
				federatedTokenFile: "",
			},
		},
		{
			name:        "invalid: no tenant id",
			expectError: true,
			opts: &configOpts{
				storageAccountName: "storageaccountname",
				containerName:      "test-container",
				clientID:           "test-client-id",
				tenantID:           "",
				clientSecret:       "client-secret",
				federatedTokenFile: "",
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validate(testCase.opts)
			if testCase.expectError && err == nil {
				t.Error("expected validation error, but didn't get one")
			} else if !testCase.expectError && err != nil {
				t.Errorf("unexpected validation error %v", err)
			}
		})
	}
}
