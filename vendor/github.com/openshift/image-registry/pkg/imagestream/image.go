package imagestream

import (
	"context"
	"fmt"

	dcontext "github.com/docker/distribution/context"
	"github.com/opencontainers/go-digest"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	imageapiv1 "github.com/openshift/api/image/v1"

	"github.com/openshift/image-registry/pkg/dockerregistry/server/client"
	rerrors "github.com/openshift/image-registry/pkg/errors"
	imageapi "github.com/openshift/image-registry/pkg/origin-common/image/apis/image"
	util "github.com/openshift/image-registry/pkg/origin-common/util"
)

const (
	ErrImageGetterCode          = "ImageGetter:"
	ErrImageGetterUnknownCode   = ErrImageGetterCode + "Unknown"
	ErrImageGetterNotFoundCode  = ErrImageGetterCode + "NotFound"
	ErrImageGetterForbiddenCode = ErrImageGetterCode + "Forbidden"
)

func IsImageManaged(image *imageapiv1.Image) bool {
	managed, ok := image.ObjectMeta.Annotations[imageapi.ManagedByOpenShiftAnnotation]
	return ok && managed == "true"
}

type imageGetter interface {
	Get(ctx context.Context, dgst digest.Digest) (*imageapiv1.Image, *rerrors.Error)
}

type cachedImageGetter struct {
	client client.Interface
	cache  map[digest.Digest]*imageapiv1.Image
}

func newCachedImageGetter(client client.Interface) imageGetter {
	return &cachedImageGetter{
		client: client,
		cache:  make(map[digest.Digest]*imageapiv1.Image),
	}
}

// Get retrieves the Image resource with the digest dgst. No authorization check is made.
func (ig *cachedImageGetter) Get(ctx context.Context, dgst digest.Digest) (*imageapiv1.Image, *rerrors.Error) {
	if image, ok := ig.cache[dgst]; ok {
		dcontext.GetLogger(ctx).Debugf("(*cachedImageGetter).Get: found image %s in cache", image.Name)
		return image, nil
	}

	image, err := ig.client.Images().Get(dgst.String(), metav1.GetOptions{})
	if err != nil {
		switch {
		case kerrors.IsNotFound(err):
			return nil, rerrors.NewError(ErrImageGetterNotFoundCode, dgst.String(), err)
		case kerrors.IsForbidden(err):
			return nil, rerrors.NewError(ErrImageGetterForbiddenCode, dgst.String(), err)
		}
		return nil, rerrors.NewError(ErrImageGetterUnknownCode, dgst.String(), err)
	}

	dcontext.GetLogger(ctx).Debugf("(*cachedImageGetter).Get: got image %s from server", image.Name)

	if err := util.ImageWithMetadata(image); err != nil {
		return nil, rerrors.NewError(
			ErrImageGetterUnknownCode,
			fmt.Sprintf("Get: unable to initialize image %s from metadata", dgst.String()),
			err,
		)
	}

	ig.cache[dgst] = image

	return image, nil
}
