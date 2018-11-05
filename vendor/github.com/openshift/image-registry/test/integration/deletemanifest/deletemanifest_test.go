package integration

import (
	"context"
	"testing"

	"github.com/opencontainers/go-digest"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	imageclientv1 "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"

	"github.com/openshift/image-registry/pkg/testframework"
	"github.com/openshift/image-registry/pkg/testutil"
)

func TestDeleteManifest(t *testing.T) {
	master := testframework.NewMaster(t)
	defer master.Close()

	testuser := master.CreateUser("testuser", "testp@ssw0rd")
	master.GrantPrunerRole(testuser)
	testproject := master.CreateProject("image-registry-test-delete-manifest", testuser.Name)
	imageStreamName := "test-delete-manifest"
	tag := "latest"

	registry := master.StartRegistry(t)
	defer registry.Close()

	repo := registry.Repository(testproject.Name, imageStreamName, testuser)

	ctx := context.Background()

	manifest, err := testutil.UploadSchema2Image(ctx, repo, tag)
	if err != nil {
		t.Fatalf("unable to upload an image: %v", err)
	}
	_, manifestPayload, err := manifest.Payload()
	if err != nil {
		t.Fatal(err)
	}
	manifestDigest := digest.FromBytes(manifestPayload)

	imageClient := imageclientv1.NewForConfigOrDie(testuser.KubeConfig())

	err = imageClient.ImageStreamTags(testproject.Name).Delete(imageStreamName+":"+tag, &metav1.DeleteOptions{})
	if err != nil {
		t.Fatalf("delete image stream tag %s:%s: %v", imageStreamName, tag, err)
	}

	ms, err := repo.Manifests(ctx)
	if err != nil {
		t.Fatal(err)
	}

	err = ms.Delete(ctx, manifestDigest)
	if err != nil {
		t.Fatalf("unable to delete manifest: %s", err)
	}
}
