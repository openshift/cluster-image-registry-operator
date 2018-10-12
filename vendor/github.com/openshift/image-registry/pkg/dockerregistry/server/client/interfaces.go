package client

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"

	imageapiv1 "github.com/openshift/api/image/v1"
	userapiv1 "github.com/openshift/api/user/v1"
	authapiv1 "k8s.io/api/authorization/v1"

	imageclientv1 "github.com/openshift/client-go/image/clientset/versioned/typed/image/v1"
	userclientv1 "github.com/openshift/client-go/user/clientset/versioned/typed/user/v1"

	authclientv1 "k8s.io/client-go/kubernetes/typed/authorization/v1"
)

type UsersInterfacer interface {
	Users() UserInterface
}

type ImagesInterfacer interface {
	Images() ImageInterface
}

type ImageSignaturesInterfacer interface {
	ImageSignatures() ImageSignatureInterface
}

type ImageStreamImagesNamespacer interface {
	ImageStreamImages(namespace string) ImageStreamImageInterface
}

type ImageStreamsNamespacer interface {
	ImageStreams(namespace string) ImageStreamInterface
}

type ImageStreamMappingsNamespacer interface {
	ImageStreamMappings(namespace string) ImageStreamMappingInterface
}

type ImageStreamSecretsNamespacer interface {
	ImageStreamSecrets(namespace string) ImageStreamSecretInterface
}

type ImageStreamTagsNamespacer interface {
	ImageStreamTags(namespace string) ImageStreamTagInterface
}

type LimitRangesGetter interface {
	LimitRanges(namespace string) LimitRangeInterface
}

type LocalSubjectAccessReviewsNamespacer interface {
	LocalSubjectAccessReviews(namespace string) LocalSubjectAccessReviewInterface
}

type SelfSubjectAccessReviewsNamespacer interface {
	SelfSubjectAccessReviews() SelfSubjectAccessReviewInterface
}

var _ ImageSignatureInterface = imageclientv1.ImageSignatureInterface(nil)

type ImageSignatureInterface interface {
	Create(signature *imageapiv1.ImageSignature) (*imageapiv1.ImageSignature, error)
}

var _ ImageStreamImageInterface = imageclientv1.ImageStreamImageInterface(nil)

type ImageStreamImageInterface interface {
	Get(name string, options metav1.GetOptions) (*imageapiv1.ImageStreamImage, error)
}

var _ UserInterface = userclientv1.UserInterface(nil)

type UserInterface interface {
	Get(name string, options metav1.GetOptions) (*userapiv1.User, error)
}

var _ ImageInterface = imageclientv1.ImageInterface(nil)

type ImageInterface interface {
	Get(name string, options metav1.GetOptions) (*imageapiv1.Image, error)
	Update(image *imageapiv1.Image) (*imageapiv1.Image, error)
	List(opts metav1.ListOptions) (*imageapiv1.ImageList, error)
}

var _ ImageStreamInterface = imageclientv1.ImageStreamInterface(nil)

type ImageStreamInterface interface {
	Get(name string, options metav1.GetOptions) (*imageapiv1.ImageStream, error)
	Create(stream *imageapiv1.ImageStream) (*imageapiv1.ImageStream, error)
	List(opts metav1.ListOptions) (*imageapiv1.ImageStreamList, error)
	Layers(name string, options metav1.GetOptions) (*imageapiv1.ImageStreamLayers, error)
}

var _ ImageStreamMappingInterface = imageclientv1.ImageStreamMappingInterface(nil)

type ImageStreamMappingInterface interface {
	Create(mapping *imageapiv1.ImageStreamMapping) (*metav1.Status, error)
}

var _ ImageStreamTagInterface = imageclientv1.ImageStreamTagInterface(nil)

type ImageStreamTagInterface interface {
	Delete(name string, options *metav1.DeleteOptions) error
}

var _ ImageStreamSecretInterface = imageclientv1.ImageStreamInterface(nil)

type ImageStreamSecretInterface interface {
	Secrets(name string, options metav1.GetOptions) (*corev1.SecretList, error)
}

var _ LimitRangeInterface = coreclientv1.LimitRangeInterface(nil)

type LimitRangeInterface interface {
	List(opts metav1.ListOptions) (*corev1.LimitRangeList, error)
}

var _ LocalSubjectAccessReviewInterface = authclientv1.LocalSubjectAccessReviewInterface(nil)

type LocalSubjectAccessReviewInterface interface {
	Create(policy *authapiv1.LocalSubjectAccessReview) (*authapiv1.LocalSubjectAccessReview, error)
}

var _ SelfSubjectAccessReviewInterface = authclientv1.SelfSubjectAccessReviewInterface(nil)

type SelfSubjectAccessReviewInterface interface {
	Create(policy *authapiv1.SelfSubjectAccessReview) (*authapiv1.SelfSubjectAccessReview, error)
}
