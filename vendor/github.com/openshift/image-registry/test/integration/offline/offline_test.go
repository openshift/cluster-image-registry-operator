package integration

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/docker/distribution/registry/storage/driver/inmemory"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	imageapiv1 "github.com/openshift/api/image/v1"
	imageclientv1 "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"

	"github.com/openshift/image-registry/pkg/testframework"
	"github.com/openshift/image-registry/pkg/testutil"
	"github.com/openshift/image-registry/test/internal/storage"
	"github.com/openshift/image-registry/test/internal/storagepath"
)

func TestOffline(t *testing.T) {
	imageData, err := testframework.NewSchema2ImageData()
	if err != nil {
		t.Fatal(err)
	}

	remoteRegistryUnavailable := false
	ts := testframework.NewHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := fmt.Sprintf("%s %s", r.Method, r.URL.Path)

		t.Logf("remote registry: %s", req)

		w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")

		if remoteRegistryUnavailable {
			http.Error(w, "service unavailable", http.StatusServiceUnavailable)
			return
		}

		if testframework.ServeV2(w, r) ||
			testframework.ServeImage(w, r, "remoteimage", imageData, []string{"latest"}) {
			return
		}

		t.Errorf("error: remote registry got unexpected request %s: %#+v", req, r)
		http.Error(w, "unable to handle the request", http.StatusInternalServerError)
	}))
	defer ts.Close()

	master := testframework.NewMaster(t)
	defer master.Close()

	testuser := master.CreateUser("testuser", "testp@ssw0rd")
	testproject := master.CreateProject("test-offline-image-pullthrough", testuser.Name)
	teststreamName := "pullthrough"

	t.Log("=== import image")

	imageClient := imageclientv1.NewForConfigOrDie(master.AdminKubeConfig())

	isi, err := imageClient.ImageStreamImports(testproject.Name).Create(&imageapiv1.ImageStreamImport{
		ObjectMeta: metav1.ObjectMeta{
			Name: teststreamName,
		},
		Spec: imageapiv1.ImageStreamImportSpec{
			Import: true,
			Images: []imageapiv1.ImageImportSpec{
				{
					From: corev1.ObjectReference{
						Kind: "DockerImage",
						Name: fmt.Sprintf("%s/remoteimage:latest", ts.URL.Host),
					},
					ImportPolicy: imageapiv1.TagImportPolicy{
						Insecure: true,
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	teststream, err := imageClient.ImageStreams(testproject.Name).Get(teststreamName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if len(teststream.Status.Tags) == 0 {
		t.Fatalf("failed to import image: %#+v %#+v", isi, teststream)
	}

	t.Log("=== mirror image")

	driver := storage.NewWaitableDriver(inmemory.New())
	registry := master.StartRegistry(t, storage.WithDriver(driver))
	defer registry.Close()

	repo := registry.Repository(testproject.Name, teststream.Name, testuser)

	ctx := context.Background()
	ctx = testutil.WithTestLogger(ctx, t)

	/* Pull the image to start mirroring */
	mediatype, dgst, err := testutil.VerifyRemoteImage(ctx, repo, "latest")
	if err != nil {
		t.Fatal(err)
	}
	if mediatype != imageData.ManifestMediaType {
		t.Fatalf("manifest mediatype: got %q, want %q", mediatype, imageData.ManifestMediaType)
	}
	if dgst != imageData.ManifestDigest {
		t.Fatalf("manifest digest: got %q, want %q", dgst, imageData.ManifestDigest)
	}

	/* Wait for mirroring to complete */
	timeoutContext, timeoutCancel := context.WithTimeout(ctx, 10*time.Second)
	if err := driver.WaitFor(
		timeoutContext,
		storagepath.Layer(testproject.Name+"/"+teststream.Name, imageData.ConfigDigest),
		storagepath.Layer(testproject.Name+"/"+teststream.Name, imageData.LayerDigest),
		storagepath.Manifest(testproject.Name+"/"+teststream.Name, imageData.ManifestDigest),
	); err != nil {
		t.Fatal(err)
	}
	timeoutCancel()

	t.Log("=== check if image pullable without remote registry")

	remoteRegistryUnavailable = true

	mediatype, dgst, err = testutil.VerifyRemoteImage(ctx, repo, "latest")
	if err != nil {
		t.Fatal(err)
	}
	if mediatype != imageData.ManifestMediaType {
		t.Fatalf("manifest mediatype: got %q, want %q", mediatype, imageData.ManifestMediaType)
	}
	if dgst != imageData.ManifestDigest {
		t.Fatalf("manifest digest: got %q, want %q", dgst, imageData.ManifestDigest)
	}
}
