package gcs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"
	configlisters "github.com/openshift/client-go/config/listers/config/v1"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"

	rscmgr "cloud.google.com/go/resourcemanager/apiv3"
	"google.golang.org/api/option"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kcorelisters "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/cache"
)

const (
	testInfraName            = "test-3748h"
	defaultInfraResourceName = "cluster"
)

var fakeResourceTags = map[string]string{
	"openshift/test1/test1": "tagValues/281478395625645",
	"openshift/test2/test2": "tagValues/281481390040765",
	"openshift/test3/test3": "tagValues/281476018424673",
	"openshift/test4/test4": "tagValues/281476661334958",
	"openshift/test5/test5": "tagValues/281475302386112",
}

// fakeTagBindingsClient is for faking tag binding operations on fake resources.
type fakeTagBindingsClient struct {
	t *testing.T

	MockDeduplicateTags   func(parent string, tagList []string) []string
	MockCreateTagBindings func(parent string, tags []string) error
	MockClose             func()
}

func NewTestTagBindingsClient(t *testing.T, ctx context.Context, httpClient *http.Client, httpEndpoint string) TagBindingsService {
	t.Helper()
	opts := []option.ClientOption{option.WithHTTPClient(httpClient), option.WithEndpoint(httpEndpoint)}
	client, err := rscmgr.NewTagBindingsRESTClient(ctx, opts...)
	if err != nil {
		t.Fatalf("failed to create test tag bindings client: %v", err)
	}
	return &tagBindingsClient{client}
}

func (f *fakeTagBindingsClient) DeduplicateTags(ctx context.Context, parent string, tagList []string) []string {
	return f.MockDeduplicateTags(parent, tagList)
}

func (f *fakeTagBindingsClient) CreateTagBindings(ctx context.Context, parent string, tags []string) error {
	return f.MockCreateTagBindings(parent, tags)
}

func (f *fakeTagBindingsClient) Close() {
	f.MockClose()
}

func getFakeListEffectiveTagsResp() []byte {
	return []byte(`{"effectiveTags":[{"tagValue":"tagValues/281483998077332","namespacedTagValue":"openshift/test3/test3","tagKey":"tagKeys/281482830535601","namespacedTagKey":"openshift/test3","inherited":true,"tagKeyParentName":"projects/openshift"},
{"tagValue":"tagValues/281478395625645","namespacedTagValue":"openshift/test1/test1","tagKey":"tagKeys/281478395625645","namespacedTagKey":"openshift/test1","inherited":true,"tagKeyParentName":"projects/openshift"}]}`)
}

func getFakeListEffectiveTagsForbiddenErrorResp(resource string) []byte {
	return []byte(fmt.Sprintf(`{"error":{"code":403,"message":"The caller does not have permission","status":"PERMISSION_DENIED","details":[{"@type":"type.googleapis.com/google.rpc.ResourceInfo","resourceName":"%s","description":"permission [resourcemanager.hierarchyNodes.listEffectiveTags] required (or the resource may not exist in this location)"}]}}`, resource))
}

func getFakeCreateTagBindingResp(parent, tagValue, tagValueNamespacedName string) []byte {
	name := fmt.Sprintf("tagBindings/%s/%s", url.PathEscape(parent), tagValue)
	return []byte(fmt.Sprintf(`{"done":true,"response":{"@type":"type.googleapis.com/google.cloud.resourcemanager.v3.TagBinding","name":"%s","parent":"%s","tagValue":"%s","tagValueNamespacedName":"%s"}}`, name, parent, tagValue, tagValueNamespacedName))
}

func getFakeCreateTagBindingOngoingResp(parent, tagValue, tagValueNamespacedName string) []byte {
	name := fmt.Sprintf("tagBindings/%s/%s", url.PathEscape(parent), tagValue)
	return []byte(fmt.Sprintf(`{"done":false,"response":{"@type":"type.googleapis.com/google.cloud.resourcemanager.v3.TagBinding","name":"%s","parent":"%s","tagValue":"%s","tagValueNamespacedName":"%s"}}`, name, parent, tagValue, tagValueNamespacedName))
}

func getFakeCreateTagBindingConflictErrorResp(tagValue string) []byte {
	return []byte(fmt.Sprintf(`{"error":{"code":409,"message":"A binding already exists between the given resource and TagValue.","status":"ALREADY_EXISTS","details":[{"@type":"type.googleapis.com/google.rpc.PreconditionFailure","violations":[{"type":"EXISTING_BINDING","subject":"//cloudresourcemanager.googleapis.com/%s","description":"Conflicting TagValue."}]}]}}`, tagValue))
}

func getFakeCreateTagBindingForbiddenErrorResp(tagValue, tagValueNamespacedName string) []byte {
	return []byte(fmt.Sprintf(`{"error":{"code":403,"message":"Permission denied on resource '%s' (or it may not exist)","status":"PERMISSION_DENIED","details":[{"@type":"type.googleapis.com/google.rpc.PreconditionFailure","violations":[{"type":"PERMISSION_DENIED","subject":"//cloudresourcemanager.googleapis.com/%s","description":"Permission Denied"}]}]}}`, tagValueNamespacedName, tagValue))
}

func getFakeTagValue(tagValueNamespacedName string) string {
	return fakeResourceTags[tagValueNamespacedName]
}

func fakeListEffectiveTagsHandler(retFailureFor interface{}, w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	if parent, ok := query["parent"]; ok {
		if len(parent) != 0 {
			scenarios := retFailureFor.(map[string]int)
			failure := scenarios[parent[0]]
			switch failure {
			case http.StatusForbidden:
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write(getFakeListEffectiveTagsForbiddenErrorResp(parent[0]))
				return
			}
		}
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(getFakeListEffectiveTagsResp())
}

func fakeCreateTagBindingHandler(retFailureFor interface{}, w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	req := new(struct {
		Parent                 string `json:"parent,omitempty"`
		TagValue               string `json:"tagValue,omitempty"`
		TagValueNamespacedName string `json:"tagValueNamespacedName,omitempty"`
	})
	if err := json.Unmarshal(body, req); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(body))

	req.TagValue = getFakeTagValue(req.TagValueNamespacedName)
	scenarios := retFailureFor.(map[string]int)
	failure := scenarios[req.TagValueNamespacedName]
	switch failure {
	case http.StatusConflict:
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write(getFakeCreateTagBindingConflictErrorResp(req.TagValue))
		return
	case http.StatusForbidden:
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write(getFakeCreateTagBindingForbiddenErrorResp(req.TagValue, req.TagValueNamespacedName))
		return
	case http.StatusAccepted:
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write(getFakeCreateTagBindingOngoingResp(req.Parent, req.TagValue, req.TagValueNamespacedName))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(getFakeCreateTagBindingResp(req.Parent, req.TagValue, req.TagValueNamespacedName))
}

func fakeAPIServerHandler(retFailureFor interface{}) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		uri := r.URL.RequestURI()
		switch {
		case strings.HasPrefix(uri, "/v3/effectiveTags?"):
			fakeListEffectiveTagsHandler(retFailureFor, w, r)
		case strings.HasPrefix(uri, "/v3/tagBindings?"):
			fakeCreateTagBindingHandler(retFailureFor, w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func NewFakeGAPIServer(retFailureFor interface{}) *httptest.Server {
	return httptest.NewTLSServer(fakeAPIServerHandler(retFailureFor))
}

func fakeSecretLister(t *testing.T, secretObj *corev1.Secret) kcorelisters.SecretNamespaceLister {
	t.Helper()
	fakeIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	if secretObj == nil {
		return kcorelisters.NewSecretLister(fakeIndexer).Secrets("default")
	}
	if err := fakeIndexer.Add(secretObj); err != nil {
		t.Fatalf("failed to create fake secret: %v", err)
	}
	return kcorelisters.NewSecretLister(fakeIndexer).Secrets(secretObj.Namespace)
}

func fakeInfrastructureLister(t *testing.T, infraObj *configv1.Infrastructure) configlisters.InfrastructureLister {
	t.Helper()
	fakeIndexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
	if infraObj == nil {
		return configlisters.NewInfrastructureLister(fakeIndexer)
	}
	if err := fakeIndexer.Add(infraObj); err != nil {
		t.Fatalf("failed to create fake infrastructure: %v", err)
	}
	return configlisters.NewInfrastructureLister(fakeIndexer)
}

func getFakeListers(t *testing.T, infraObj *configv1.Infrastructure, secretObj *corev1.Secret) *regopclient.StorageListers {
	t.Helper()
	return &regopclient.StorageListers{
		Infrastructures: fakeInfrastructureLister(t, infraObj),
		Secrets:         fakeSecretLister(t, secretObj),
	}
}

func getCredJSON(t *testing.T) []byte {
	t.Helper()
	credJSON, err := json.Marshal(map[string]string{
		"type":           "service_account",
		"project_id":     "project-id",
		"private_key_id": "key-id",
		"client_email":   "service-account-email",
		"client_id":      "client-id",
	})
	if err != nil {
		t.Fatalf("error marshalling config json: %v", err)
	}
	return credJSON
}

func errorAsExpected(wantErr string, gotErr error) bool {
	return (len(wantErr) == 0 && gotErr == nil) ||
		(len(wantErr) != 0 && gotErr != nil && gotErr.Error() == wantErr)
}

func conditionAsExpected(wantCond operatorapi.OperatorCondition, gotConds []operatorapi.OperatorCondition) bool {
	for _, c := range gotConds {
		if wantCond.Type != c.Type {
			continue
		}
		if c.Reason != wantCond.Reason ||
			c.Message != wantCond.Message ||
			c.Status != wantCond.Status {
			return false
		}
		return true
	}
	return false
}

func slicesEqual(src1, src2 []string) bool {
	if len(src1) != len(src2) {
		return false
	}

	matchedCount := 0
	for _, s1 := range src1 {
		for _, s2 := range src2 {
			if s1 == s2 {
				matchedCount++
				break
			}
		}
	}

	return matchedCount == len(src1)
}

func TestGetUserLabels(t *testing.T) {
	infraRef := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{
			Name: defaultInfraResourceName,
		},
		Spec: configv1.InfrastructureSpec{
			PlatformSpec: configv1.PlatformSpec{
				Type: configv1.GCPPlatformType,
			},
		},
		Status: configv1.InfrastructureStatus{
			InfrastructureName: testInfraName,
			PlatformStatus: &configv1.PlatformStatus{
				Type: configv1.GCPPlatformType,
				GCP:  &configv1.GCPPlatformStatus{},
			},
		},
	}

	for _, tt := range []struct {
		name           string
		getInfraObj    func() *configv1.Infrastructure
		expectedLabels map[string]string
		expectedError  string
	}{
		{
			name:           "infrastructure/cluster resource does not exist",
			getInfraObj:    func() *configv1.Infrastructure { return nil },
			expectedLabels: nil,
			expectedError:  `getUserLabels: failed to read infrastructure/cluster resource: infrastructure.config.openshift.io "cluster" not found`,
		},
		{
			name: "userLabels not defined in infrastructure/cluster resource",
			getInfraObj: func() *configv1.Infrastructure {
				infra := new(configv1.Infrastructure)
				infraRef.DeepCopyInto(infra)
				infra.Status.PlatformStatus.GCP.ResourceLabels = []configv1.GCPResourceLabel{}
				return infra
			},
			expectedLabels: map[string]string{
				fmt.Sprintf("kubernetes-io-cluster-%s", testInfraName): "owned",
			},
		},
		{
			name: "userLabels defined in infrastructure/cluster resource",
			getInfraObj: func() *configv1.Infrastructure {
				infra := new(configv1.Infrastructure)
				infraRef.DeepCopyInto(infra)
				infra.Status.PlatformStatus.GCP.ResourceLabels = []configv1.GCPResourceLabel{
					{
						Key:   "key1",
						Value: "value1",
					},
					{
						Key:   "key2",
						Value: "value2",
					},
				}
				return infra
			},
			expectedLabels: map[string]string{
				"key1": "value1",
				"key2": "value2",
				fmt.Sprintf("kubernetes-io-cluster-%s", testInfraName): "owned",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			infraLister := fakeInfrastructureLister(t, tt.getInfraObj())
			labels, err := getUserLabels(infraLister)
			if !errorAsExpected(tt.expectedError, err) {
				t.Errorf("getUserLabels(): error: want: %v, got: %v", tt.expectedError, err)
			}
			if !reflect.DeepEqual(labels, tt.expectedLabels) {
				t.Errorf("getUserLabels(): labels: want: %v, got: %v", tt.expectedLabels, labels)
			}
		})
	}
}

func TestAddTagsToStorageBucket(t *testing.T) {
	var (
		infraRef = &configv1.Infrastructure{
			ObjectMeta: metav1.ObjectMeta{
				Name: defaultInfraResourceName,
			},
			Spec: configv1.InfrastructureSpec{
				PlatformSpec: configv1.PlatformSpec{
					Type: configv1.GCPPlatformType,
				},
			},
			Status: configv1.InfrastructureStatus{
				InfrastructureName: testInfraName,
				PlatformStatus: &configv1.PlatformStatus{
					Type: configv1.GCPPlatformType,
					GCP:  &configv1.GCPPlatformStatus{},
				},
			},
		}
		secretObj = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      defaults.CloudCredentialsName,
				Namespace: defaults.ImageRegistryOperatorNamespace,
			},
			Data: map[string][]byte{
				"service_account.json": getCredJSON(t),
			},
		}
		imageRegistryConfigRef = &imageregistryv1.Config{
			Spec: imageregistryv1.ImageRegistrySpec{
				Storage: imageregistryv1.ImageRegistryConfigStorage{
					GCS: &imageregistryv1.ImageRegistryConfigStorageGCS{
						Bucket:    "test-bucket",
						Region:    "asia-south1-a",
						ProjectID: "project-id",
					},
				},
			},
		}
	)

	tagMgr := &tagServiceManager{
		tagBindingsClient: &fakeTagBindingsClient{
			t: t,
			MockDeduplicateTags: func(parent string, tagList []string) []string {
				switch parent {
				case "//storage.googleapis.com/projects/_/buckets/test-bucket3":
					return []string{
						"openshift/key1/value1",
						"openshift/key2/value2",
					}
				case "//storage.googleapis.com/projects/_/buckets/test-bucket4",
					"//storage.googleapis.com/projects/_/buckets/test-bucket5":
					return []string{
						"openshift/key1/value1",
					}
				}
				return nil
			},
			MockCreateTagBindings: func(parent string, tags []string) error {
				switch parent {
				case "//storage.googleapis.com/projects/_/buckets/test-bucket5":
					return fmt.Errorf("failed to add tags to test-bucket5 resource")
				}
				return nil
			},
			MockClose: func() {},
		},
	}
	defer tagMgr.Close()

	for _, tt := range []struct {
		name                    string
		bucketName              string
		region                  string
		getInfraObj             func() *configv1.Infrastructure
		expectedStatusCondition operatorapi.OperatorCondition
		expectedError           string
	}{
		{
			name:        "infrastructure/cluster resource does not exist",
			bucketName:  "test-bucket1",
			getInfraObj: func() *configv1.Infrastructure { return nil },
			expectedStatusCondition: operatorapi.OperatorCondition{
				Type:    defaults.StorageTagged,
				Status:  operatorapi.ConditionFalse,
				Reason:  gcpTagsFailedStatusReason,
				Message: `failed to read infrastructure/cluster resource: infrastructure.config.openshift.io "cluster" not found`,
			},
			expectedError: `failed to read infrastructure/cluster resource: infrastructure.config.openshift.io "cluster" not found`,
		},
		{
			name:       "userTags not defined in infrastructure/cluster resource",
			bucketName: "test-bucket2",
			getInfraObj: func() *configv1.Infrastructure {
				infra := new(configv1.Infrastructure)
				infraRef.DeepCopyInto(infra)
				infra.Status.PlatformStatus.GCP.ResourceTags = nil
				return infra
			},
			expectedStatusCondition: operatorapi.OperatorCondition{
				Type:    defaults.StorageTagged,
				Status:  operatorapi.ConditionFalse,
				Reason:  gcpTagsSuccessStatusReason,
				Message: UserTagsNotDefined.Error(),
			},
			expectedError: `user did not define any tags`,
		},
		{
			name:       "userTags defined in infrastructure/cluster resource",
			bucketName: "test-bucket3",
			getInfraObj: func() *configv1.Infrastructure {
				infra := new(configv1.Infrastructure)
				infraRef.DeepCopyInto(infra)
				infra.Status.PlatformStatus.GCP.ResourceTags = []configv1.GCPResourceTag{
					{
						ParentID: "openshift",
						Key:      "key1",
						Value:    "value1",
					},
					{
						ParentID: "openshift",
						Key:      "key2",
						Value:    "value2",
					},
				}
				return infra
			},
			expectedStatusCondition: operatorapi.OperatorCondition{
				Type:    defaults.StorageTagged,
				Status:  operatorapi.ConditionTrue,
				Reason:  gcpTagsSuccessStatusReason,
				Message: `Successfully added user-defined tags to test-bucket3 storage bucket`,
			},
		},
		{
			name:       "adding tags to bucket fails(mock error)",
			bucketName: "test-bucket5",
			getInfraObj: func() *configv1.Infrastructure {
				infra := new(configv1.Infrastructure)
				infraRef.DeepCopyInto(infra)
				infra.Status.PlatformStatus.GCP.ResourceTags = []configv1.GCPResourceTag{
					{
						ParentID: "openshift",
						Key:      "key1",
						Value:    "value1",
					},
				}
				return infra
			},
			expectedStatusCondition: operatorapi.OperatorCondition{
				Type:    defaults.StorageTagged,
				Status:  operatorapi.ConditionFalse,
				Reason:  gcpTagsFailedStatusReason,
				Message: `failed to add tags to test-bucket5 resource`,
			},
			expectedError: `failed to add tags to test-bucket5 resource`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			imageRegistryConfig := new(imageregistryv1.Config)
			imageRegistryConfigRef.DeepCopyInto(imageRegistryConfig)
			if tt.region != "" {
				imageRegistryConfig.Spec.Storage.GCS.Region = tt.region
			}
			if tt.bucketName != "" {
				imageRegistryConfig.Spec.Storage.GCS.Bucket = tt.bucketName
			}

			tagMgr.Listers = getFakeListers(t, tt.getInfraObj(), secretObj)
			err := tagMgr.AddTagsToStorageBucket(context.Background(), imageRegistryConfig)
			_ = updateTagCondition(imageRegistryConfig, err)
			if !errorAsExpected(tt.expectedError, err) {
				t.Errorf("AddTagsToStorageBucket(): error: want: %v, got: %v", tt.expectedError, err)
			}
			if !conditionAsExpected(tt.expectedStatusCondition, imageRegistryConfig.Status.Conditions) {
				t.Errorf("AddTagsToStorageBucket(): ImageRegistryConfig: want: %v, got: %v", tt.expectedStatusCondition, imageRegistryConfig.Status.Conditions)
			}
		})
	}
}

func TestNewTagManager(t *testing.T) {
	var (
		infraObj = &configv1.Infrastructure{
			ObjectMeta: metav1.ObjectMeta{
				Name: defaultInfraResourceName,
			},
			Spec: configv1.InfrastructureSpec{
				PlatformSpec: configv1.PlatformSpec{
					Type: configv1.GCPPlatformType,
				},
			},
			Status: configv1.InfrastructureStatus{
				InfrastructureName: testInfraName,
				PlatformStatus: &configv1.PlatformStatus{
					Type: configv1.GCPPlatformType,
					GCP:  &configv1.GCPPlatformStatus{},
				},
			},
		}
		secretObj = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      defaults.CloudCredentialsName,
				Namespace: defaults.ImageRegistryOperatorNamespace,
			},
			Data: map[string][]byte{
				"service_account.json": getCredJSON(t),
			},
		}
	)

	for _, tt := range []struct {
		name          string
		getListers    func() *regopclient.StorageListers
		expectedError string
	}{
		{
			name: "tag manager init fails, infrastructure/cluster resource does not exist",
			getListers: func() *regopclient.StorageListers {
				return getFakeListers(t, nil, secretObj)
			},
			expectedError: `failed to create tag binding client options: failed to read GCS configuration for creating tag client: infrastructure.config.openshift.io "cluster" not found`,
		},
		{
			name: "tag manager init pass",
			getListers: func() *regopclient.StorageListers {
				return getFakeListers(t, infraObj, secretObj)
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			tagMgr, err := NewTagManager(context.Background(), tt.getListers(), "asia-south1")
			if err == nil {
				defer tagMgr.Close()
			}
			if !errorAsExpected(tt.expectedError, err) {
				t.Errorf("NewTagManager(): error: want: %v, got: %v", tt.expectedError, err)
			}
		})
	}
}

func TestCreateTagBindings(t *testing.T) {
	ctx := context.Background()

	server := NewFakeGAPIServer(map[string]int{
		"openshift/test3/test3": http.StatusConflict,
		"openshift/test4/test4": http.StatusAccepted,
		"openshift/test5/test5": http.StatusForbidden,
	})
	defer server.Close()

	tagClient := NewTestTagBindingsClient(t, ctx, server.Client(), server.URL)
	defer tagClient.Close()

	for _, tt := range []struct {
		name          string
		tagsList      []string
		expectedError string
	}{
		{
			name: "adding tags fails with tag already exist error",
			tagsList: []string{
				"openshift/test1/test1",
				"openshift/test3/test3",
			},
		},
		{
			name: "adding tags fails with permission error",
			tagsList: []string{
				"openshift/test1/test1",
				"openshift/test5/test5",
			},
			expectedError: `failed to add tag(s) to //storage.googleapis.com/projects/_/buckets/test-bucket resource`,
		},
		{
			name: "adding tags fails with wait error",
			tagsList: []string{
				"openshift/test1/test1",
				"openshift/test4/test4",
			},
			expectedError: `failed to add tag(s) to //storage.googleapis.com/projects/_/buckets/test-bucket resource`,
		},
		{
			name: "added tags with no errors",
			tagsList: []string{
				"openshift/test1/test1",
				"openshift/test4/test2",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parent := fmt.Sprintf(bucketParentPathFmt, "test-bucket")
			err := tagClient.CreateTagBindings(ctx, parent, tt.tagsList)
			if !errorAsExpected(tt.expectedError, err) {
				t.Errorf("CreateTagBindings(): error: want: %v, got: %v", tt.expectedError, err)
			}
		})
	}
}

func TestDeduplicateTags(t *testing.T) {
	ctx := context.Background()

	server := NewFakeGAPIServer(map[string]int{
		fmt.Sprintf(bucketParentPathFmt, "test-bucket3"): http.StatusForbidden,
	})
	defer server.Close()

	tagClient := NewTestTagBindingsClient(t, ctx, server.Client(), server.URL)
	defer tagClient.Close()

	for _, tt := range []struct {
		name         string
		bucketName   string
		tagsList     []string
		expectedTags []string
	}{
		{
			name:       "user provided and resource inherited tags has duplicates",
			bucketName: "test-bucket1",
			tagsList: []string{
				"openshift/test1/test1",
				"openshift/test4/test4",
			},
			expectedTags: []string{
				"openshift/test4/test4",
			},
		},
		{
			name:       "user provided and resource inherited tags has no duplicates",
			bucketName: "test-bucket2",
			tagsList: []string{
				"openshift/test2/test2",
				"openshift/test5/test5",
			},
			expectedTags: []string{
				"openshift/test2/test2",
				"openshift/test5/test5",
			},
		},
		{
			name:       "fetching effective tags fails with permission error",
			bucketName: "test-bucket3",
			tagsList: []string{
				"openshift/test1/test1",
				"openshift/test4/test4",
			},
			expectedTags: []string{
				"openshift/test1/test1",
				"openshift/test4/test4",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parent := fmt.Sprintf(bucketParentPathFmt, tt.bucketName)
			tags := tagClient.DeduplicateTags(ctx, parent, tt.tagsList)
			if !slicesEqual(tt.expectedTags, tags) {
				t.Errorf("CreateTagBindings(): error: want: %v, got: %v", tt.expectedTags, tags)
			}
		})
	}
}
