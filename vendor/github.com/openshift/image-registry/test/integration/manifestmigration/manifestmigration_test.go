package integration

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/docker/distribution/registry/storage/driver/inmemory"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"

	imageapiv1 "github.com/openshift/api/image/v1"
	imageclientv1 "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"

	imageapi "github.com/openshift/image-registry/pkg/origin-common/image/apis/image"
	"github.com/openshift/image-registry/pkg/testframework"
	"github.com/openshift/image-registry/pkg/testutil"
	"github.com/openshift/image-registry/test/internal/storage"
	"github.com/openshift/image-registry/test/internal/storagepath"
)

func TestManifestMigration(t *testing.T) {
	imageData, err := testframework.NewSchema2ImageData()
	if err != nil {
		t.Fatal(err)
	}

	master := testframework.NewMaster(t)
	defer master.Close()

	testuser := master.CreateUser("testuser", "testp@ssw0rd")
	testproject := master.CreateProject("test-manifest-migration", testuser.Name)
	teststreamName := "manifestmigration"

	imageClient := imageclientv1.NewForConfigOrDie(master.AdminKubeConfig())

	_, err = imageClient.ImageStreams(testproject.Name).Create(&imageapiv1.ImageStream{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testproject.Name,
			Name:      teststreamName,
		},
	})
	if err != nil && !kerrors.IsAlreadyExists(err) {
		t.Fatal(err)
	}

	err = imageClient.Images().Delete(imageData.ManifestDigest.String(), &metav1.DeleteOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		t.Fatalf("failed to delete an old instance of the image: %v", err)
	}

	_, err = imageClient.ImageStreamMappings(testproject.Name).Create(&imageapiv1.ImageStreamMapping{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testproject.Name,
			Name:      teststreamName,
		},
		Image: imageapiv1.Image{
			ObjectMeta: metav1.ObjectMeta{
				Name: imageData.ManifestDigest.String(),
				Annotations: map[string]string{
					imageapi.ManagedByOpenShiftAnnotation: "true",
				},
			},
			DockerImageReference:         "shouldnt-be-resolved.example.com/this-is-a-fake-image",
			DockerImageManifestMediaType: imageData.ManifestMediaType,
			DockerImageManifest:          string(imageData.Manifest),
			DockerImageConfig:            string(imageData.Config),
		},
		Tag: "latest",
	})
	if err != nil {
		t.Fatalf("failed to create image stream mapping: %v", err)
	}

	driver := storage.NewWaitableDriver(inmemory.New())
	registry := master.StartRegistry(t, storage.WithDriver(driver))
	defer registry.Close()

	repo := registry.Repository(testproject.Name, teststreamName, testuser)

	ctx := context.Background()
	ctx = testutil.WithTestLogger(ctx, t)

	ms, err := repo.Manifests(ctx)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ms.Get(ctx, imageData.ManifestDigest)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("waiting for migration to finish...")

	if err := driver.WaitFor(ctx, storagepath.Blob(imageData.ManifestDigest)); err != nil {
		t.Fatal(err)
	}

	t.Logf("manifest is migrated, checking results...")

	manifestOnStorage, err := driver.GetContent(ctx, storagepath.Blob(imageData.ManifestDigest))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(manifestOnStorage, imageData.Manifest) {
		t.Errorf("migration has changed the manifest: got %q, want %q", manifestOnStorage, imageData.Manifest)
	}

	w, err := imageClient.Images().Watch(metav1.ListOptions{
		Watch: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = watch.Until(30*time.Second, w, func(event watch.Event) (bool, error) {
		if event.Type != "MODIFIED" {
			return false, nil
		}
		image, ok := event.Object.(*imageapiv1.Image)
		if !ok {
			return false, nil
		}
		if image.Name != imageData.ManifestDigest.String() || image.DockerImageManifest != "" && image.DockerImageConfig != "" {
			return false, nil
		}
		return true, nil
	})
	if err != nil {
		t.Fatalf("waiting for the manifest and the config to be removed from the image: %v", err)
	}
}
