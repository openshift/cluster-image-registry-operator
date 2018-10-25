package v1alpha1

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorsv1alpha1api "github.com/openshift/api/operator/v1alpha1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ImageRegistryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []ImageRegistry `json:"items"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ImageRegistry struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              ImageRegistrySpec   `json:"spec"`
	Status            ImageRegistryStatus `json:"status,omitempty"`
}

type ImageRegistryConfigProxy struct {
	HTTP  string `json:"http,omitempty"`
	HTTPS string `json:"https,omitempty"`
}

type ImageRegistryConfigStorageS3 struct {
	Bucket         string `json:"bucket,omitempty"`
	Region         string `json:"region,omitempty"`
	RegionEndpoint string `json:"regionEndpoint,omitempty"`
	Encrypt        bool   `json:"encrypt,omitempty"`
}

type ImageRegistryConfigStorageAzure struct {
	Container string `json:"container,omitempty"`
}

type ImageRegistryConfigStorageGCS struct {
	Bucket string `json:"bucket,omitempty"`
}

type ImageRegistryConfigStorageSwift struct {
	AuthURL   string `json:"authURL,omitempty"`
	Container string `json:"container,omitempty"`
}

type ImageRegistryConfigStorageFilesystem struct {
	VolumeSource corev1.VolumeSource `json:"volumeSource,omitempty"`
}

type ImageRegistryConfigStorage struct {
	Azure      *ImageRegistryConfigStorageAzure      `json:"azure,omitempty"`
	Filesystem *ImageRegistryConfigStorageFilesystem `json:"filesystem,omitempty"`
	GCS        *ImageRegistryConfigStorageGCS        `json:"gcs,omitempty"`
	S3         *ImageRegistryConfigStorageS3         `json:"s3,omitempty"`
	Swift      *ImageRegistryConfigStorageSwift      `json:"swift,omitempty"`
}

type ImageRegistryConfigRequestsLimits struct {
	MaxRunning     int           `json:"maxrunning,omitempty"`
	MaxInQueue     int           `json:"maxinqueue,omitempty"`
	MaxWaitInQueue time.Duration `json:"maxwaitinqueue,omitempty"`
}

type ImageRegistryConfigRequests struct {
	Read  ImageRegistryConfigRequestsLimits `json:"read,omitempty"`
	Write ImageRegistryConfigRequestsLimits `json:"write,omitempty"`
}

type ImageRegistryConfigRoute struct {
	Name       string `json:"name"`
	Hostname   string `json:"hostname"`
	SecretName string `json:"secretName"`
}

type ImageRegistrySpec struct {
	operatorsv1alpha1api.OperatorSpec `json:",inline"`

	HTTPSecret   string                      `json:"httpSecret,omitempty"`
	Proxy        ImageRegistryConfigProxy    `json:"proxy,omitempty"`
	Storage      ImageRegistryConfigStorage  `json:"storage,omitempty"`
	Requests     ImageRegistryConfigRequests `json:"requests,omitempty"`
	TLS          bool                        `json:"tls,omitempty"`
	DefaultRoute bool                        `json:"defaultRoute,omitempty"`
	Routes       []ImageRegistryConfigRoute  `json:"routes,omitempty"`
	Replicas     int32                       `json:"replicas,omitempty"`
}

type ImageRegistryStatus struct {
	operatorsv1alpha1api.OperatorStatus `json:",inline"`

	InternalRegistryHostname string `json:"internalRegistryHostname"`
}
