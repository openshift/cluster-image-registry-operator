package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"k8s.io/klog/v2"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/blob"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/bloberror"
	"github.com/Azure/azure-sdk-for-go/sdk/storage/azblob/container"
	"github.com/Azure/go-autorest/autorest/azure"
)

func main() {
	opts := getConfigOpts()
	if err := validate(opts); err != nil {
		log.Fatal(err)
	}

	// if the environment specific configs are not given, assume
	//  AzurePublicCloud as it's probably the most common choice anyway.
	if len(opts.environment) == 0 {
		opts.environment = "AZUREPUBLICCLOUD"
	}

	if err := createASHEnvironmentFile(opts); err != nil {
		log.Fatal(err)
	}

	cloudConfig, err := getCloudConfig(opts.environment)
	if err != nil {
		log.Fatal(err)
	}

	client, err := getClient(cloudConfig, opts)
	if err != nil {
		log.Fatal(err)
	}
	ctx := context.Background()
	_, err = moveBlobs(ctx, client, &moveBlobOpts{
		source: "docker",
		dest:   "/docker",
	})
	if err != nil {
		log.Fatal(err)
	}
}

type moveBlobOpts struct {
	source string
	dest   string
}

// moveBlobs moves blobs from o.source to o.dest.
//
// moveBlobs will first copy blobs from o.source to o.dest, then delete the
// successfully copied blobs from o.source.
// If o.source has a lot of blobs, this function could take a while to finish.
func moveBlobs(
	ctx context.Context,
	containerClient *container.Client,
	o *moveBlobOpts,
) ([]string, error) {
	sourceBlobs, err := listBlobs(ctx, containerClient, o.source)
	if err != nil {
		return []string{}, err
	}
	klog.Infof("found %d blobs to move", len(sourceBlobs))

	// we gather errors so that when they happen we still have a shot
	// of copying some blobs into the destination, which allows for
	// incremental retries on error.
	errors := []error{}
	copiesToWaitFor := map[string]blob.CopyStatusType{}
	movedBlobs := []string{}

	for _, sourceBlobName := range sourceBlobs {
		// rename the source blob to match the destination.
		// we're dealing with virtual paths(dirs) here, so the path
		// is part of the blob name.
		destBlobName := strings.Replace(sourceBlobName, o.source, o.dest, 1)

		klog.V(3).Infof("transforced source blob name from %q into %q", sourceBlobName, destBlobName)

		// the blob client represents the destination blob, so we use
		// blob renamed to match the destination.
		blobClient := containerClient.NewBlobClient(destBlobName)

		// the source blob has to be on the same container as the
		// destination blob for this to work.
		// it's name MUST be escaped.
		// we also ensure there's a "/" separating the URL from the
		// source blob name so the container name doesn't get mixed up
		// with the source blob name.
		sourceBlobURL := strings.TrimRight(containerClient.URL(), "/") + "/" + url.QueryEscape(sourceBlobName)

		// counter-intuitively, this copy uses the blob which this client
		// is created for as the destination, and the source is given in
		// the call to StartCopyFromURL.
		klog.Infof("starting copy of %q", sourceBlobName)
		resp, err := blobClient.StartCopyFromURL(ctx, sourceBlobURL, nil)
		if err != nil {
			errors = append(errors, fmt.Errorf("failed to start copy: %v", err))
			continue
		}

		switch *resp.CopyStatus {
		case blob.CopyStatusTypeSuccess:
			klog.Infof("copy finished instantly for blob %q", sourceBlobName)
			movedBlobs = append(movedBlobs, sourceBlobName)
		case blob.CopyStatusTypeAborted, blob.CopyStatusTypeFailed:
			klog.Warningf("copy failed failed for blob %q, moving on", sourceBlobName)
			errors = append(errors,
				fmt.Errorf("copy failed with status %q for blob %q", *resp.CopyStatus, sourceBlobName))
			// leave retry up to the client. in the image-registry case, the k8s job
			// will handle retrying after failures.
		case blob.CopyStatusTypePending:
			klog.Infof("copy is pending for blob %q, adding to list of copies to wait for", sourceBlobName)
			copiesToWaitFor[destBlobName] = *resp.CopyStatus
		}
	}

	// this code is very difficult to exercise. none of my attempts to
	// force an asynchronous copy worked, no matter how big the source file
	// was. I was forced to manipulate the code in a way that exercised
	// loop a few times to ensure it worked.
	for blobName, copyStatus := range copiesToWaitFor {
		blobClient := containerClient.NewBlobClient(blobName)
		for copyStatus == blob.CopyStatusTypePending {
			props, err := blobClient.GetProperties(ctx, nil)
			if err != nil {
				errors = append(errors, err)
				continue
			}
			copyStatus = *props.CopyStatus
			if copyStatus == blob.CopyStatusTypeAborted || copyStatus == blob.CopyStatusTypeFailed {
				err := fmt.Errorf("copy failed, status: %q, blob: %q", *props.CopyStatus, blobName)
				if props.CopyStatusDescription != nil {
					err = fmt.Errorf(
						"copy failed, status: %q, desc: %q, blob: %q",
						copyStatus,
						*props.CopyStatusDescription,
						blobName,
					)
				}
				errors = append(errors, err)
				continue
			}
			if copyStatus == blob.CopyStatusTypePending {
				// copy still pending - wait an arbitraty amount of time before trying again
				klog.Infof("waiting 100ms before re-checking copy status for blob %q", blobName)
				time.Sleep(100 * time.Millisecond)
			}
		}
		sourceBlobName := strings.Replace(blobName, o.dest, o.source, 1)
		klog.V(3).Infof("adding blob to moved blobs list: %q", sourceBlobName)
		movedBlobs = append(movedBlobs, sourceBlobName)
	}

	// only delete source blobs we know have been moved
	for _, blobName := range movedBlobs {
		blobClient := containerClient.NewBlobClient(blobName)
		_, err := blobClient.Delete(ctx, nil)
		if err != nil && !bloberror.HasCode(err, bloberror.BlobNotFound) {
			errors = append(errors, fmt.Errorf("failed deleting copied blob: %v", err))
		}
		klog.Infof("deleted copied blob from source %q", blobName)
	}

	klog.Infof("moved %d blobs", len(movedBlobs))
	if len(errors) > 0 {
		return movedBlobs, fmt.Errorf("encountered errors when moving blobs: %v", errors)
	}
	return movedBlobs, nil
}

func listBlobs(
	ctx context.Context,
	containerClient *container.Client,
	prefix string,
) ([]string, error) {
	blobs := []string{}
	pager := containerClient.NewListBlobsFlatPager(&container.ListBlobsFlatOptions{
		Prefix: &prefix,
	})
	for pager.More() {
		resp, err := pager.NextPage(ctx)
		if err != nil {
			return []string{}, err
		}
		if resp.Segment == nil {
			return []string{}, fmt.Errorf("response has no segments")
		}
		for _, blob := range resp.Segment.BlobItems {
			if blob.Name == nil {
				return []string{}, fmt.Errorf(
					"required blob property Name is missing while listing blobs under: %s",
					prefix,
				)
			}
			blobs = append(blobs, *blob.Name)

		}
	}
	return blobs, nil
}

type configOpts struct {
	storageAccountName string
	containerName      string
	clientID           string
	tenantID           string
	clientSecret       string
	federatedTokenFile string
	accountKey         string
	environment        string
	// environmentFilePath and environmentFileContents are specific
	// for Azure Stack Hub
	environmentFilePath     string
	environmentFileContents string
}

func createASHEnvironmentFile(opts *configOpts) error {
	if len(opts.environmentFilePath) == 0 || len(opts.environmentFileContents) == 0 {
		klog.Info("Azure Stack Hub environment variables not present in current environment, skipping setup...")
		return nil
	}
	f, err := os.Create(opts.environmentFilePath)
	if err != nil {
		return err
	}

	_, err = f.WriteString(opts.environmentFileContents)
	if err != nil {
		f.Close()
		os.Remove(f.Name())
		return err
	}

	err = f.Close()
	if err != nil {
		return err
	}
	return nil
}

func getCloudConfig(environment string) (cloud.Configuration, error) {
	env, err := azure.EnvironmentFromName(environment)
	if err != nil {
		return cloud.Configuration{}, err
	}
	return cloud.Configuration{
		ActiveDirectoryAuthorityHost: env.ActiveDirectoryEndpoint,
		Services: map[cloud.ServiceName]cloud.ServiceConfiguration{
			cloud.ResourceManager: {
				Audience: env.TokenAudience,
				Endpoint: env.ResourceManagerEndpoint,
			},
		},
	}, nil
}

func getConfigOpts() *configOpts {
	return &configOpts{
		storageAccountName:      strings.TrimSpace(os.Getenv("AZURE_STORAGE_ACCOUNT_NAME")),
		containerName:           strings.TrimSpace(os.Getenv("AZURE_CONTAINER_NAME")),
		clientID:                strings.TrimSpace(os.Getenv("AZURE_CLIENT_ID")),
		tenantID:                strings.TrimSpace(os.Getenv("AZURE_TENANT_ID")),
		clientSecret:            strings.TrimSpace(os.Getenv("AZURE_CLIENT_SECRET")),
		federatedTokenFile:      strings.TrimSpace(os.Getenv("AZURE_FEDERATED_TOKEN_FILE")),
		accountKey:              strings.TrimSpace(os.Getenv("AZURE_ACCOUNTKEY")),
		environment:             strings.TrimSpace(os.Getenv("AZURE_ENVIRONMENT")),
		environmentFilePath:     strings.TrimSpace(os.Getenv("AZURE_ENVIRONMENT_FILEPATH")),
		environmentFileContents: strings.TrimSpace(os.Getenv("AZURE_ENVIRONMENT_FILECONTENTS")),
	}
}

// getCreds build credentials from the given parameters.
//
// this function is basically copy of what the operator itself does,
// as a way to ensure that it will work in the same way as the operator.
func getClient(cloudConfig cloud.Configuration, opts *configOpts) (*container.Client, error) {
	env, err := azure.EnvironmentFromName(opts.environment)
	if err != nil {
		return nil, err
	}
	containerURL := fmt.Sprintf(
		"https://%s.blob.%s/%s",
		opts.storageAccountName,
		env.StorageEndpointSuffix,
		opts.containerName,
	)
	var client *container.Client
	clientOpts := azcore.ClientOptions{
		Cloud: cloudConfig,
	}

	if len(opts.accountKey) > 0 {
		cred, err := container.NewSharedKeyCredential(opts.storageAccountName, opts.accountKey)
		if err != nil {
			return nil, err
		}
		client, err = container.NewClientWithSharedKeyCredential(containerURL, cred, &container.ClientOptions{ClientOptions: clientOpts})
		if err != nil {
			return nil, err
		}
	} else if len(opts.clientSecret) > 0 {
		options := azidentity.ClientSecretCredentialOptions{
			ClientOptions: azcore.ClientOptions{
				Cloud: cloudConfig,
			},
		}
		cred, err := azidentity.NewClientSecretCredential(opts.tenantID, opts.clientID, opts.clientSecret, &options)
		if err != nil {
			return nil, err
		}
		client, err = container.NewClient(containerURL, cred, &container.ClientOptions{ClientOptions: clientOpts})
		if err != nil {
			return nil, err
		}
	} else if len(opts.federatedTokenFile) > 0 {
		options := azidentity.WorkloadIdentityCredentialOptions{
			ClientOptions: azcore.ClientOptions{
				Cloud: cloudConfig,
			},
			ClientID:      opts.clientID,
			TenantID:      opts.tenantID,
			TokenFilePath: opts.federatedTokenFile,
		}
		cred, err := azidentity.NewWorkloadIdentityCredential(&options)
		if err != nil {
			return nil, err
		}
		client, err = container.NewClient(containerURL, cred, &container.ClientOptions{ClientOptions: clientOpts})
		if err != nil {
			return nil, err
		}
	} else {
		options := azidentity.DefaultAzureCredentialOptions{
			ClientOptions: azcore.ClientOptions{
				Cloud: cloudConfig,
			},
		}
		cred, err := azidentity.NewDefaultAzureCredential(&options)
		if err != nil {
			return nil, err
		}
		client, err = container.NewClient(containerURL, cred, &container.ClientOptions{ClientOptions: clientOpts})
		if err != nil {
			return nil, err
		}
	}
	return client, nil
}

// validate returns an error when the required options are missing.
func validate(opts *configOpts) error {
	if len(opts.clientSecret) == 0 && len(opts.federatedTokenFile) == 0 && len(opts.accountKey) == 0 {
		return fmt.Errorf("One of AZURE_CLIENT_SECRET or AZURE_FEDERATED_TOKEN_FILE or AZURE_ACCOUNTKEY is required for authentication")
	}
	if len(opts.clientID) == 0 && len(opts.accountKey) == 0 {
		return fmt.Errorf("AZURE_CLIENT_ID is required for authentication")
	}
	if len(opts.tenantID) == 0 && len(opts.accountKey) == 0 {
		return fmt.Errorf("AZURE_TENANT_ID is required for authentication")
	}
	if len(opts.storageAccountName) == 0 {
		return fmt.Errorf("AZURE_STORAGE_ACCOUNT_NAME is required")
	}
	if len(opts.containerName) == 0 {
		return fmt.Errorf("AZURE_CONTAINER_NAME is required")
	}
	return nil
}
