package integration

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	imageapiv1 "github.com/openshift/api/image/v1"
	imageclientv1 "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"

	"github.com/openshift/image-registry/pkg/testframework"
	"github.com/openshift/image-registry/pkg/testutil/counter"
)

func TestPullthroughBlob(t *testing.T) {
	imageData, err := testframework.NewSchema2ImageData()
	if err != nil {
		t.Fatal(err)
	}

	master := testframework.NewMaster(t)
	defer master.Close()

	testuser := master.CreateUser("testuser", "testp@ssw0rd")
	testproject := master.CreateProject("test-image-pullthrough-blob", testuser.Name)
	teststreamName := "pullthrough"

	requestCounter := counter.New()
	ts := testframework.NewHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := fmt.Sprintf("%s %s", r.Method, r.URL.Path)

		t.Logf("remote registry: %s", req)
		requestCounter.Add(req, 1)

		w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")

		if testframework.ServeV2(w, r) ||
			testframework.ServeImage(w, r, "remoteimage", imageData, []string{"latest"}) {
			return
		}

		t.Errorf("error: remote registry got unexpected request %s: %#+v", req, r)
		http.Error(w, "unable to handle the request", http.StatusInternalServerError)
	}))
	defer ts.Close()

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

	if diff := requestCounter.Diff(counter.M{
		"GET /v2/":                                                     1,
		"GET /v2/remoteimage/manifests/latest":                         1,
		"GET /v2/remoteimage/blobs/" + imageData.ConfigDigest.String(): 1,
	}); diff != nil {
		t.Fatalf("unexpected number of requests: %q", diff)
	}

	// Reset counter
	requestCounter = counter.New()

	registry := master.StartRegistry(t, testframework.DisableMirroring{})
	defer registry.Close()

	repo := registry.Repository(testproject.Name, teststream.Name, testuser)

	ctx := context.Background()

	data, err := repo.Blobs(ctx).Get(ctx, imageData.LayerDigest)
	if err != nil {
		t.Fatal(err)
	}

	if string(data) != string(imageData.Layer) {
		t.Fatalf("got %q, want %q", string(data), string(imageData.Layer))
	}

	// TODO(dmage): remove the HEAD request
	if diff := requestCounter.Diff(counter.M{
		"GET /v2/": 1,
		"HEAD /v2/remoteimage/blobs/" + imageData.LayerDigest.String(): 1,
		"GET /v2/remoteimage/blobs/" + imageData.LayerDigest.String():  1,
	}); diff != nil {
		t.Fatalf("unexpected number of requests: %q", diff)
	}
}
