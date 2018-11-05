package integration

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/docker/distribution"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	imageapiv1 "github.com/openshift/api/image/v1"
	imageclientv1 "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"

	"github.com/openshift/image-registry/pkg/testframework"
)

func TestPullthroughBlob(t *testing.T) {
	imageData, err := testframework.NewSchema2ImageData()
	if err != nil {
		t.Fatal(err)
	}

	master := testframework.NewMaster(t)
	defer master.Close()

	testuser := master.CreateUser("testuser", "testp@ssw0rd")
	testproject := master.CreateProject("testproject", testuser.Name)
	teststreamName := "pullthrough"

	// TODO(dmage): use atomic variable
	remoteRegistryRequiresAuth := false
	ts := testframework.NewHTTPServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := fmt.Sprintf("%s %s", r.Method, r.URL.Path)

		t.Logf("remote registry: %s", req)

		w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")

		if remoteRegistryRequiresAuth {
			w.WriteHeader(http.StatusUnauthorized)
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

	remoteRegistryRequiresAuth = true

	registry := master.StartRegistry(t, testframework.DisableMirroring{}, testframework.EnableMetrics{Secret: "MetricsSecret"})
	defer registry.Close()

	repo := registry.Repository(testproject.Name, teststream.Name, testuser)

	ctx := context.Background()

	_, err = repo.Blobs(ctx).Get(ctx, imageData.LayerDigest)
	if err != distribution.ErrBlobUnknown {
		t.Fatal(err)
	}

	req, err := http.NewRequest("GET", registry.BaseURL()+"/extensions/v2/metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer MetricsSecret")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	metrics := []struct {
		name   string
		values []string
	}{
		{
			name:   "imageregistry_storage_duration_seconds_bucket",
			values: []string{`operation="StorageDriver.Stat"`},
		},
		{
			name:   "imageregistry_storage_errors_total",
			values: []string{`operation="StorageDriver.Stat"`, `code="PATH_NOT_FOUND"`},
		},
		{
			name:   "imageregistry_pullthrough_repository_errors_total",
			values: []string{`operation="BlobStore.Stat"`, `code="UNAUTHORIZED"`},
		},
		{
			name:   "imageregistry_pullthrough_blobstore_cache_requests_total",
			values: []string{`type="Miss"`},
		},
		{
			name:   "imageregistry_pullthrough_repository_duration_seconds_bucket",
			values: []string{`operation="Init"`},
		},
	}

	r := bufio.NewReader(resp.Body)
	for {
		line, err := r.ReadString('\n')
		line = strings.TrimRight(line, "\n")
		t.Log(line)

	metric:
		for i, m := range metrics {
			if !strings.HasPrefix(line, m.name+"{") {
				continue
			}
			for _, v := range m.values {
				if !strings.Contains(line, v) {
					continue metric
				}
			}

			// metric found, delete it
			metrics[i] = metrics[len(metrics)-1]
			metrics = metrics[:len(metrics)-1]
			break
		}

		if err == io.EOF {
			break
		} else if err != nil {
			t.Fatal(err)
		}
	}
	if len(metrics) != 0 {
		t.Fatalf("unable to find metrics: %v", metrics)
	}
}
