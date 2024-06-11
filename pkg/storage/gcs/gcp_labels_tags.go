package gcs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	imageregistryv1 "github.com/openshift/api/imageregistry/v1"
	operatorapi "github.com/openshift/api/operator/v1"
	configlisters "github.com/openshift/client-go/config/listers/config/v1"
	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
	"github.com/openshift/cluster-image-registry-operator/pkg/defaults"
	"github.com/openshift/cluster-image-registry-operator/pkg/storage/util"

	rscmgr "cloud.google.com/go/resourcemanager/apiv3"
	rscmgrpb "cloud.google.com/go/resourcemanager/apiv3/resourcemanagerpb"
	"github.com/googleapis/gax-go/v2"
	"github.com/googleapis/gax-go/v2/apierror"
	"golang.org/x/time/rate"
	"google.golang.org/api/iterator"
	"google.golang.org/api/option"

	"k8s.io/klog/v2"
)

const (
	// ocpDefaultLabelFmt is the format string for the default label
	// added to the OpenShift created GCP resources.
	ocpDefaultLabelFmt = "kubernetes-io-cluster-%s"

	// gcpTagsSuccessStatusReason is the operator condition status reason
	// for successful tag operations.
	gcpTagsSuccessStatusReason = "SuccessTaggingBucket"

	// gcpTagsFailedStatusReason is the operator condition status reason
	// for failed tag operations.
	gcpTagsFailedStatusReason = "ErrorTaggingBucket"

	// gcpMaxTagsPerResource is the maximum number of tags that can
	// be attached to a resource.
	// https://cloud.google.com/resource-manager/docs/limits#tag-limits
	gcpMaxTagsPerResource = 50

	// gcpTagsRequestRateLimit is the tag request rate limit per second.
	gcpTagsRequestRateLimit = 8

	// gcpTagsRequestTokenBucketSize is the burst/token bucket size used
	// for limiting API requests.
	gcpTagsRequestTokenBucketSize = 8

	// resourceManagerHostSubPath is the endpoint for tag requests.
	resourceManagerHostSubPath = "cloudresourcemanager.googleapis.com"

	// bucketParentPathFmt is the string format for the parent path of a bucket resource
	bucketParentPathFmt = "//storage.googleapis.com/projects/_/buckets/%s"
)

// UserTagsNotDefined is returned when user defined tags is empty; used for updating
// status condition.
var UserTagsNotDefined = errors.New("user did not define any tags")

// TagService is the interface that wraps methods for resource tag operations.
type TagService interface {
	AddTagsToStorageBucket(context.Context, *imageregistryv1.Config) error
	Close()
}

// TagBindingsService is the interface that wraps methods for resource tag binding operations.
type TagBindingsService interface {
	DeduplicateTags(context.Context, string, []string) []string
	CreateTagBindings(context.Context, string, []string) error
	Close()
}

// tagServiceManager handles resource tagging.
type tagServiceManager struct {
	Listers           *regopclient.StorageListers
	tagBindingsClient TagBindingsService
}

// tagBindingsClient handles resource tag bindings.
type tagBindingsClient struct {
	*rscmgr.TagBindingsClient
}

// getUserLabels returns the user defined labels in status subresource of
// infrastructure/cluster resource, along with the default labels defined in OCP.
func getUserLabels(infraLister configlisters.InfrastructureLister) (map[string]string, error) {
	infra, err := util.GetInfrastructure(infraLister)
	if err != nil {
		return nil, fmt.Errorf("getUserLabels: failed to read infrastructure/cluster resource: %w", err)
	}
	// add OCP default label along with user-defined labels
	labels := map[string]string{
		fmt.Sprintf(ocpDefaultLabelFmt, infra.Status.InfrastructureName): "owned",
	}
	// get user-defined labels in Infrastructure.Status.GCP
	if infra.Status.PlatformStatus != nil &&
		infra.Status.PlatformStatus.GCP != nil &&
		infra.Status.PlatformStatus.GCP.ResourceLabels != nil {
		for _, label := range infra.Status.PlatformStatus.GCP.ResourceLabels {
			labels[label.Key] = label.Value
		}
	}
	return labels, nil
}

// newRequestLimiter returns token bucket based request rate limiter after initializing
// the passed values for limit, burst(or token bucket) size. If opted for emptyBucket
// all initial tokens are reserved for the first burst.
func newRequestLimiter(limit, burst int, emptyBucket bool) *rate.Limiter {
	limiter := rate.NewLimiter(rate.Every(time.Second/time.Duration(limit)), burst)

	if emptyBucket {
		limiter.AllowN(time.Now(), burst)
	}

	return limiter
}

// getTagCreateCallOptions returns a list of additional call options to use for
// the tag binding create operations.
func getTagCreateCallOptions() []gax.CallOption {
	const (
		initialRetryDelay    = 90 * time.Second
		maxRetryDuration     = 5 * time.Minute
		retryDelayMultiplier = 2.0
	)

	return []gax.CallOption{
		gax.WithRetry(func() gax.Retryer {
			return gax.OnHTTPCodes(gax.Backoff{
				Initial:    initialRetryDelay,
				Max:        maxRetryDuration,
				Multiplier: retryDelayMultiplier,
			},
				http.StatusTooManyRequests)
		}),
	}
}

// DeduplicateTags returns the filtered list of tags by removing tags
// inherited by the resource from its parent resource.
func (c *tagBindingsClient) DeduplicateTags(ctx context.Context, resourceName string, tagList []string) []string {
	dupTags := make(map[string]bool, len(tagList))
	for _, k := range tagList {
		dupTags[k] = false
	}

	bindings := c.listEffectiveTags(ctx, resourceName)
	// a resource can have a maximum of {gcpMaxTagsPerResource} tags attached to it.
	// Will iterate for {gcpMaxTagsPerResource} times in the worst case scenario, if
	// none of the break conditions are met. Should the {gcpMaxTagsPerResource} be
	// increased in the future, it should not create an issue, since this is an optimization
	// attempt to reduce the number the tag write calls by skipping already existing tags,
	// since it has a quota restriction.
	for i := 0; i < gcpMaxTagsPerResource; i++ {
		binding, err := bindings.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil || binding == nil {
			// on encountering any error will continue adding refined tags
			// which will still have all the user provided tags except for
			// the removed already existing tags processed until this point
			// and would end up adding tags which already exist, and error
			// handling is present for the same.
			klog.V(5).Infof("failed to list effective tags on the %s resource: %v: %v", resourceName, binding, err)
			break
		}
		tag := binding.GetNamespacedTagValue()
		if _, exist := dupTags[tag]; exist {
			dupTags[tag] = true
			klog.V(4).Infof("filterTagList: skipping tag %s already exists on the %s resource", tag, resourceName)
		}
	}

	filteredTags := make([]string, 0, len(tagList))
	for tagValue, dup := range dupTags {
		if !dup {
			filteredTags = append(filteredTags, tagValue)
		}
	}

	return filteredTags
}

// toTagValueList converts the tags to an array containing tagValues
// NamespacedNames.
func toTagValueList(tags []configv1.GCPResourceTag) []string {
	if len(tags) <= 0 {
		return nil
	}

	list := make([]string, 0, len(tags))
	for _, tag := range tags {
		t := fmt.Sprintf("%s/%s/%s", tag.ParentID, tag.Key, tag.Value)
		list = append(list, t)
	}
	return list
}

// getInfraResourceTagsList returns the user-defined tags present in the
// status sub-resource of Infrastructure.
func getInfraResourceTagsList(platformStatus *configv1.PlatformStatus) []configv1.GCPResourceTag {
	if platformStatus != nil && platformStatus.GCP != nil && platformStatus.GCP.ResourceTags != nil {
		return platformStatus.GCP.ResourceTags
	}
	klog.V(1).Infof("getInfraResourceTagsList: user-defined tag list is not provided")
	return nil
}

// getTagsToBind returns list of user tags defined in status subresource of
// infrastructure/cluster resource, after removing the tags which already exist
// on the gcp resource.
func (t *tagServiceManager) getTagsToBind(ctx context.Context, bucketName string) ([]string, error) {
	infra, err := util.GetInfrastructure(t.Listers.Infrastructures)
	if err != nil {
		return nil, fmt.Errorf("failed to read infrastructure/cluster resource: %w", err)
	}

	infraTags := toTagValueList(getInfraResourceTagsList(infra.Status.PlatformStatus))

	return t.tagBindingsClient.DeduplicateTags(ctx, bucketName, infraTags), nil
}

// listEffectiveTags is a method that wraps GAPI ListEffectiveTags.
func (c *tagBindingsClient) listEffectiveTags(ctx context.Context, resourceName string) *rscmgr.EffectiveTagIterator {
	return c.TagBindingsClient.ListEffectiveTags(ctx, &rscmgrpb.ListEffectiveTagsRequest{
		Parent: resourceName,
	})
}

// createTagBinding is a method that wraps GAPI CreateTagBinding.
func (c *tagBindingsClient) createTagBinding(ctx context.Context, resourceName, tag string) (*rscmgr.CreateTagBindingOperation, error) {
	op, err := c.TagBindingsClient.CreateTagBinding(ctx, &rscmgrpb.CreateTagBindingRequest{
		TagBinding: &rscmgrpb.TagBinding{
			Parent:                 resourceName,
			TagValueNamespacedName: tag,
		},
	}, getTagCreateCallOptions()...)
	return op, err
}

// wait is a method that wraps GAPI Wait.
func (c *tagBindingsClient) wait(ctx context.Context, op *rscmgr.CreateTagBindingOperation) error {
	_, err := op.Wait(ctx)
	return err
}

// CreateTagBindings creates the tag bindings for the resource.
func (c *tagBindingsClient) CreateTagBindings(ctx context.Context, resourceName string, tags []string) error {
	// GCP has a rate limit of 600 requests per minute, restricting
	// here to 8 requests per second.
	limiter := newRequestLimiter(gcpTagsRequestRateLimit, gcpTagsRequestTokenBucketSize, true)

	errFlag := false
	for _, tag := range tags {
		if err := limiter.Wait(ctx); err != nil {
			errFlag = true
			klog.Errorf("rate limiting request to add %s tag to %s resource failed: %v",
				tag, resourceName, err)
			continue
		}

		result, err := c.createTagBinding(ctx, resourceName, tag)
		if err != nil {
			var gErr *apierror.APIError
			if errors.As(err, &gErr) && gErr.HTTPCode() == http.StatusConflict {
				klog.V(5).Infof("tag %s already exist on %s resource", tag, resourceName)
				continue
			}
			errFlag = true
			klog.Errorf("request to add %s tag to %s resource failed: %v", tag, resourceName, err)
			continue
		}

		if err = c.wait(ctx, result); err != nil {
			errFlag = true
			klog.Errorf("failed to add %s tag to %s resource: %v", tag, resourceName, err)
		}
		klog.Infof("successfully added %s tag to %s resource", tag, resourceName)
	}
	if errFlag {
		return fmt.Errorf("failed to add tag(s) to %s resource", resourceName)
	}

	return nil
}

// addTagsToStorageBucket adds the user-defined tags in the Infrastructure resource
// to the passed GCP bucket resource.
func (t *tagServiceManager) addTagsToStorageBucket(ctx context.Context, cr *imageregistryv1.Config) error {
	bucketFullName := fmt.Sprintf(bucketParentPathFmt, cr.Spec.Storage.GCS.Bucket)
	tags, err := t.getTagsToBind(ctx, bucketFullName)
	if err != nil {
		return err
	}
	if len(tags) <= 0 {
		return UserTagsNotDefined
	}

	if err := t.tagBindingsClient.CreateTagBindings(ctx, bucketFullName, tags); err != nil {
		return err
	}

	return nil
}

// getTagClientOptions returns the tag client options adding the credentials and
// the endpoint which will be used by the client.
func getTagClientOptions(listers *regopclient.StorageListers, endpoint string) ([]option.ClientOption, error) {
	cfg, err := GetConfig(listers)
	if err != nil {
		return nil, fmt.Errorf("failed to read GCS configuration for creating tag client: %w", err)
	}

	opts := []option.ClientOption{
		option.WithCredentialsJSON([]byte(cfg.KeyfileData)),
		option.WithEndpoint(endpoint),
	}

	return opts, nil
}

// getTagBindingsClient returns the client to be used for creating tag bindings to
// the resources.
func getTagBindingsClient(ctx context.Context, listers *regopclient.StorageListers, location string) (TagBindingsService, error) {
	endpoint := fmt.Sprintf("https://%s-%s", location, resourceManagerHostSubPath)
	opts, err := getTagClientOptions(listers, endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to create tag binding client options: %w", err)
	}

	client, err := rscmgr.NewTagBindingsRESTClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create tag binding client: %w", err)
	}
	return &tagBindingsClient{client}, nil
}

// NewTagManager creates a tagServiceManager instance. Explicit Close() must
// be called when tag service is no longer needed to Close() all clients.
func NewTagManager(ctx context.Context, listers *regopclient.StorageListers, region string) (TagService, error) {
	client, err := getTagBindingsClient(ctx, listers, region)
	if err != nil || client == nil {
		return nil, err
	}
	mgr := &tagServiceManager{
		Listers:           listers,
		tagBindingsClient: client,
	}
	return mgr, nil
}

// Close the tag bindings client connection to API server.
func (c *tagBindingsClient) Close() {
	if err := c.TagBindingsClient.Close(); err != nil {
		klog.Errorf("failed to close tag binding client: %v", err)
	}
}

// Close the connections created by tag manager.
func (t *tagServiceManager) Close() {
	t.tagBindingsClient.Close()
}

// AddTagsToStorageBucket adds the user-defined tags in the Infrastructure resource
// to the passed GCP bucket resource. It's wrapper around addUserTagsToStorageBucket()
// and additionally updates status condition in image registry Config resource.
func (t *tagServiceManager) AddTagsToStorageBucket(ctx context.Context, cr *imageregistryv1.Config) error {
	return t.addTagsToStorageBucket(ctx, cr)
}

// updateTagCondition will update or add the `StorageTagged` condition.
func updateTagCondition(cr *imageregistryv1.Config, err error) error {
	if err != nil {
		if errors.Is(err, UserTagsNotDefined) {
			util.UpdateCondition(cr, defaults.StorageTagged, operatorapi.ConditionFalse,
				gcpTagsSuccessStatusReason, UserTagsNotDefined.Error())
			return nil
		}
		util.UpdateCondition(cr, defaults.StorageTagged, operatorapi.ConditionFalse,
			gcpTagsFailedStatusReason, err.Error())
		return err
	}

	util.UpdateCondition(cr, defaults.StorageTagged, operatorapi.ConditionTrue,
		gcpTagsSuccessStatusReason,
		fmt.Sprintf("Successfully added user-defined tags to %s storage bucket", cr.Spec.Storage.GCS.Bucket))
	return nil
}
