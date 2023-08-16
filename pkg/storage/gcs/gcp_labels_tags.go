package gcs

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
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

// newLimiter returns token bucket based request rate limiter after initializing
// the passed values for limit, burst(or token bucket) size. If opted for emptyBucket
// all initial tokens are reserved for the first burst.
func newLimiter(limit, burst int, emptyBucket bool) *rate.Limiter {
	limiter := rate.NewLimiter(rate.Every(time.Second/time.Duration(limit)), burst)

	if emptyBucket {
		limiter.AllowN(time.Now(), burst)
	}

	return limiter
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

// getTagsList returns the list of tags to apply on the resources.
func getTagsList(platformStatus *configv1.PlatformStatus) []string {
	return toTagValueList(getInfraResourceTagsList(platformStatus))
}

// getFilteredTagList returns the list of tags to apply on the resources after
// filtering the tags already existing on a given resource.
func getFilteredTagList(ctx context.Context, platformStatus *configv1.PlatformStatus, client *rscmgr.TagBindingsClient, parent string) []string {
	return filterTagList(ctx, client, parent, getTagsList(platformStatus))
}

// filterTagList returns the filtered list of tags to apply on the resources.
func filterTagList(ctx context.Context, client *rscmgr.TagBindingsClient, parent string, tagList []string) []string {
	dupTags := make(map[string]bool, len(tagList))
	for _, k := range tagList {
		dupTags[k] = false
	}

	listBindingsReq := &rscmgrpb.ListEffectiveTagsRequest{
		Parent: parent,
	}
	bindings := client.ListEffectiveTags(ctx, listBindingsReq)
	// a resource can have a maximum of {gcpMaxTagsPerResource} tags attached to it.
	// Will iterate for {gcpMaxTagsPerResource} times in the worst case scenario, if
	// none of the break conditions are met. Should the {gcpMaxTagsPerResource} be
	// increased in future, it should not create an issue, since this is an optimization
	// attempt to reduce the number the tag write calls by skipping already existing tags,
	// since it has a quota restriction.
	for i := 0; i < gcpMaxTagsPerResource; i++ {
		binding, err := bindings.Next()
		if errors.Is(err, iterator.Done) {
			break
		}
		if err != nil || binding == nil {
			klog.V(4).Infof("failed to list effective tags on the %s bucket: %v: %v", parent, binding, err)
			break
		}
		tag := binding.GetNamespacedTagValue()
		if _, exist := dupTags[tag]; exist {
			dupTags[tag] = true
			klog.V(4).Infof("filterTagList: skipping tag %s already exists on the %s bucket", tag, parent)
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

// getCreateCallOptions returns a list of additional call options to use for
// the create operations.
func getCreateCallOptions() []gax.CallOption {
	return []gax.CallOption{
		gax.WithRetry(func() gax.Retryer {
			return gax.OnHTTPCodes(gax.Backoff{
				Initial:    90 * time.Second,
				Max:        5 * time.Minute,
				Multiplier: 2,
			},
				http.StatusTooManyRequests)
		}),
	}
}

// getTagBindingsClient returns the client to be used for creating tag bindings to
// the resources.
func getTagBindingsClient(ctx context.Context, listers *regopclient.StorageListers, location string) (*rscmgr.TagBindingsClient, error) {
	cfg, err := GetConfig(listers)
	if err != nil {
		return nil, fmt.Errorf("getTagBindingsClient: failed to read gcp config: %w", err)
	}

	endpoint := fmt.Sprintf("https://%s-%s", location, resourceManagerHostSubPath)
	opts := []option.ClientOption{
		option.WithCredentialsJSON([]byte(cfg.KeyfileData)),
		option.WithEndpoint(endpoint),
	}
	return rscmgr.NewTagBindingsRESTClient(ctx, opts...)
}

// addTagsToStorageBucket adds the user-defined tags in the Infrastructure resource
// to the passed GCP bucket resource. It's wrapper around addUserTagsToStorageBucket()
// additionally updates status condition.
func addTagsToStorageBucket(ctx context.Context, cr *imageregistryv1.Config, listers *regopclient.StorageListers, bucketName, region string) error {
	if err := addUserTagsToStorageBucket(ctx, listers, bucketName, region); err != nil {
		util.UpdateCondition(cr, defaults.StorageTagged, operatorapi.ConditionFalse,
			gcpTagsFailedStatusReason, err.Error())
		return err
	}
	util.UpdateCondition(cr, defaults.StorageTagged, operatorapi.ConditionTrue,
		gcpTagsSuccessStatusReason,
		fmt.Sprintf("Successfully added user-defined tags to %s storage bucket", bucketName))
	return nil
}

// addUserTagsToStorageBucket adds the user-defined tags in the Infrastructure resource
// to the passed GCP bucket resource.
func addUserTagsToStorageBucket(ctx context.Context, listers *regopclient.StorageListers, bucketName, region string) error {
	// Tags are not supported for buckets located in the us-east2 and us-east3 regions.
	// https://cloud.google.com/storage/docs/tags-and-labels#tags
	if strings.ToLower(region) == "us-east2" ||
		strings.ToLower(region) == "us-east3" {
		klog.Infof("addUserTagsToStorageBucket: skip tagging bucket %s created in tags unsupported region %s", bucketName, region)
		return nil
	}

	infra, err := util.GetInfrastructure(listers.Infrastructures)
	if err != nil {
		return fmt.Errorf("addUserTagsToStorageBucket: failed to read infrastructure/cluster resource: %w", err)
	}

	client, err := getTagBindingsClient(ctx, listers, region)
	if err != nil || client == nil {
		return fmt.Errorf("failed to create tag binding client for adding tags to %s bucket: %v",
			bucketName, err)
	}
	defer client.Close()

	parent := fmt.Sprintf(bucketParentPathFmt, bucketName)
	tagValues := getFilteredTagList(ctx, infra.Status.PlatformStatus, client, parent)
	if len(tagValues) <= 0 {
		return nil
	}

	// GCP has a rate limit of 600 requests per minute, restricting
	// here to 8 requests per second.
	limiter := newLimiter(gcpTagsRequestRateLimit, gcpTagsRequestTokenBucketSize, true)

	tagBindingReq := &rscmgrpb.CreateTagBindingRequest{
		TagBinding: &rscmgrpb.TagBinding{
			Parent: parent,
		},
	}
	errFlag := false
	for _, value := range tagValues {
		if err := limiter.Wait(ctx); err != nil {
			errFlag = true
			klog.Errorf("rate limiting request to add %s tag to %s bucket failed: %v",
				value, bucketName, err)
			continue
		}

		tagBindingReq.TagBinding.TagValueNamespacedName = value
		result, err := client.CreateTagBinding(ctx, tagBindingReq, getCreateCallOptions()...)
		if err != nil {
			e, ok := err.(*apierror.APIError)
			if ok && e.HTTPCode() == http.StatusConflict {
				klog.Infof("tag binding %s/%s already exists", bucketName, value)
				continue
			}
			errFlag = true
			klog.Errorf("request to add %s tag to %s bucket failed: %v", value, bucketName, err)
			continue
		}

		if _, err = result.Wait(ctx); err != nil {
			errFlag = true
			klog.Errorf("failed to add %s tag to %s bucket: %v", value, bucketName, err)
		}
		klog.V(1).Infof("binding tag %s to %s bucket successful", value, bucketName)
	}
	if errFlag {
		return fmt.Errorf("failed to add tag(s) to %s bucket", bucketName)
	}
	return nil
}
