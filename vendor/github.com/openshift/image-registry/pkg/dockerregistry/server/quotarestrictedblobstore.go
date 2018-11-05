// Package server wraps repository and blob store objects of docker/distribution upstream. Most significantly,
// the wrappers cause manifests to be stored in OpenShift's etcd store instead of registry's storage.
// Registry's middleware API is utilized to register the object factories.
//
// Module with quotaRestrictedBlobStore defines a wrapper for upstream blob store that does an image quota and
// limits check before committing image layer to a registry. Master server contains admission check that will
// refuse the manifest if the image exceeds whatever quota or limit set. But the check occurs too late (after
// the layers are written). This addition allows us to refuse the layers and thus keep the storage clean.
//
// *Note*: Here, we take into account just a single layer, not the image as a whole because the layers are
// uploaded before the manifest. This leads to a situation where several layers can be written until a big
// enough layer will be received that exceeds the limit.
package server

import (
	"context"
	"fmt"

	"github.com/docker/distribution"
	dcontext "github.com/docker/distribution/context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/openshift/image-registry/pkg/dockerregistry/server/configuration"
	"github.com/openshift/image-registry/pkg/imagestream"
	imageapi "github.com/openshift/image-registry/pkg/origin-common/image/apis/image"
)

// newQuotaEnforcingConfig creates caches for quota objects. The objects are stored with given eviction
// timeout. Caches will only be initialized if the given ttl is positive. Options are gathered from
// configuration file and will be overridden by enforceQuota and projectCacheTTL environment variable values.
func newQuotaEnforcingConfig(ctx context.Context, quotaCfg *configuration.Quota) *quotaEnforcingConfig {
	if !quotaCfg.Enabled {
		dcontext.GetLogger(ctx).Info("quota enforcement disabled")
		return &quotaEnforcingConfig{}
	}

	if quotaCfg.CacheTTL <= 0 {
		dcontext.GetLogger(ctx).Info("not using project caches for quota objects")
		return &quotaEnforcingConfig{
			enforcementEnabled: true,
		}
	}

	dcontext.GetLogger(ctx).Infof("caching project quota objects with TTL %s", quotaCfg.CacheTTL.String())
	return &quotaEnforcingConfig{
		enforcementEnabled: true,
		limitRanges:        newProjectObjectListCache(quotaCfg.CacheTTL),
	}
}

// quotaEnforcingConfig holds configuration and caches of object lists keyed by project name. Caches are
// thread safe and shall be reused by all middleware layers.
type quotaEnforcingConfig struct {
	// if set, enables quota enforcement
	enforcementEnabled bool
	// if set, enables caching of quota objects per project
	limitRanges imagestream.ProjectObjectListStore
}

// quotaRestrictedBlobStore wraps upstream blob store with a guard preventing big layers exceeding image quotas
// from being saved.
type quotaRestrictedBlobStore struct {
	distribution.BlobStore

	repo *repository
}

var _ distribution.BlobStore = &quotaRestrictedBlobStore{}

// Create wraps returned blobWriter with quota guard wrapper.
func (bs *quotaRestrictedBlobStore) Create(ctx context.Context, options ...distribution.BlobCreateOption) (distribution.BlobWriter, error) {
	dcontext.GetLogger(ctx).Debug("(*quotaRestrictedBlobStore).Create: starting")

	bw, err := bs.BlobStore.Create(ctx, options...)
	if err != nil {
		return nil, err
	}

	repo := (*bs.repo)
	repo.ctx = ctx
	return &quotaRestrictedBlobWriter{
		BlobWriter: bw,
		repo:       &repo,
	}, nil
}

// Resume wraps returned blobWriter with quota guard wrapper.
func (bs *quotaRestrictedBlobStore) Resume(ctx context.Context, id string) (distribution.BlobWriter, error) {
	dcontext.GetLogger(ctx).Debug("(*quotaRestrictedBlobStore).Resume: starting")

	bw, err := bs.BlobStore.Resume(ctx, id)
	if err != nil {
		return nil, err
	}

	repo := (*bs.repo)
	repo.ctx = ctx
	return &quotaRestrictedBlobWriter{
		BlobWriter: bw,
		repo:       &repo,
	}, nil
}

// quotaRestrictedBlobWriter wraps upstream blob writer with a guard preventig big layers exceeding image
// quotas from being written.
type quotaRestrictedBlobWriter struct {
	distribution.BlobWriter

	repo *repository
}

func (bw *quotaRestrictedBlobWriter) Commit(ctx context.Context, provisional distribution.Descriptor) (canonical distribution.Descriptor, err error) {
	dcontext.GetLogger(ctx).Debug("(*quotaRestrictedBlobWriter).Commit: starting")

	if err := admitBlobWrite(ctx, bw.repo, provisional.Size); err != nil {
		return distribution.Descriptor{}, err
	}

	return bw.BlobWriter.Commit(ctx, provisional)
}

// admitBlobWrite checks whether the blob does not exceed image limit ranges if set. Returns ErrAccessDenied
// error if the limit is exceeded.
func admitBlobWrite(ctx context.Context, repo *repository, size int64) error {
	if size < 1 {
		return nil
	}

	lrs, err := repo.imageStream.GetLimitRangeList(ctx, repo.app.quotaEnforcing.limitRanges)
	if err != nil {
		return err
	}

	for _, limitrange := range lrs.Items {
		dcontext.GetLogger(ctx).Debugf("processing limit range %s/%s", limitrange.Namespace, limitrange.Name)
		for _, limit := range limitrange.Spec.Limits {
			if err := admitImage(size, limit); err != nil {
				dcontext.GetLogger(ctx).Errorf("refusing to write blob exceeding limit range %s: %s", limitrange.Name, err.Error())
				return distribution.ErrAccessDenied
			}
		}
	}

	// TODO(1): admit also against openshift.io/ImageStream quota resource when we have image stream cache in the
	// registry
	// TODO(2): admit also against openshift.io/imagestreamimages and openshift.io/imagestreamtags resources once
	// we have image stream cache in the registry

	return nil
}

// admitImage checks if the size is greater than the limit range.
func admitImage(size int64, limit corev1.LimitRangeItem) error {
	if limit.Type != imageapi.LimitTypeImage {
		return nil
	}

	limitQuantity, ok := limit.Max[corev1.ResourceStorage]
	if !ok {
		return nil
	}

	imageQuantity := resource.NewQuantity(size, resource.BinarySI)
	if limitQuantity.Cmp(*imageQuantity) < 0 {
		// image size is larger than the permitted limit range max size, image is forbidden
		return fmt.Errorf("requested usage of %s exceeds the maximum limit per %s (%s > %s)", corev1.ResourceStorage, imageapi.LimitTypeImage, imageQuantity.String(), limitQuantity.String())
	}
	return nil
}
