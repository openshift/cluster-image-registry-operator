package imagestream

import (
	"fmt"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	imageapiv1 "github.com/openshift/api/image/v1"

	"github.com/openshift/image-registry/pkg/dockerregistry/server/client"
	rerrors "github.com/openshift/image-registry/pkg/errors"
	quotautil "github.com/openshift/image-registry/pkg/origin-common/quota/util"
)

const (
	ErrImageStreamGetterCode          = "ImageStreamGetter:"
	ErrImageStreamGetterUnknownCode   = ErrImageStreamGetterCode + "Unknown"
	ErrImageStreamGetterNotFoundCode  = ErrImageStreamGetterCode + "NotFound"
	ErrImageStreamGetterForbiddenCode = ErrImageStreamGetterCode + "Forbidden"
)

// cachedImageStreamGetter wraps a master API client for getting image streams with a cache.
type cachedImageStreamGetter struct {
	namespace               string
	name                    string
	isNamespacer            client.ImageStreamsNamespacer
	cachedImageStream       *imageapiv1.ImageStream
	cachedImageStreamLayers *imageapiv1.ImageStreamLayers
}

func (g *cachedImageStreamGetter) get() (*imageapiv1.ImageStream, *rerrors.Error) {
	if g.cachedImageStream != nil {
		return g.cachedImageStream, nil
	}
	is, err := g.isNamespacer.ImageStreams(g.namespace).Get(g.name, metav1.GetOptions{})
	if err != nil {
		switch {
		case kerrors.IsNotFound(err):
			return nil, rerrors.NewError(ErrImageStreamGetterNotFoundCode, fmt.Sprintf("%s/%s", g.namespace, g.name), err)
		case kerrors.IsForbidden(err), kerrors.IsUnauthorized(err), quotautil.IsErrorQuotaExceeded(err):
			return nil, rerrors.NewError(ErrImageStreamGetterForbiddenCode, fmt.Sprintf("%s/%s", g.namespace, g.name), err)
		default:
			return nil, rerrors.NewError(ErrImageStreamGetterUnknownCode, fmt.Sprintf("%s/%s", g.namespace, g.name), err)
		}
	}

	g.cachedImageStream = is
	return is, nil
}

func (g *cachedImageStreamGetter) layers() (*imageapiv1.ImageStreamLayers, *rerrors.Error) {
	if g.cachedImageStreamLayers != nil {
		return g.cachedImageStreamLayers, nil
	}
	is, err := g.isNamespacer.ImageStreams(g.namespace).Layers(g.name, metav1.GetOptions{})
	if err != nil {
		switch {
		case kerrors.IsNotFound(err):
			return nil, rerrors.NewError(ErrImageStreamGetterNotFoundCode, fmt.Sprintf("%s/%s", g.namespace, g.name), err)
		case kerrors.IsForbidden(err), kerrors.IsUnauthorized(err), quotautil.IsErrorQuotaExceeded(err):
			return nil, rerrors.NewError(ErrImageStreamGetterForbiddenCode, fmt.Sprintf("%s/%s", g.namespace, g.name), err)
		default:
			return nil, rerrors.NewError(ErrImageStreamGetterUnknownCode, fmt.Sprintf("%s/%s", g.namespace, g.name), err)
		}
	}

	g.cachedImageStreamLayers = is
	return is, nil
}

func (g *cachedImageStreamGetter) cacheImageStream(is *imageapiv1.ImageStream) {
	g.cachedImageStream = is
}
