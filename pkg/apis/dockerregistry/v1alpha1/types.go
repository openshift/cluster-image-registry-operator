package v1alpha1

import (
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorsv1alpha1api "github.com/openshift/api/operator/v1alpha1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type OpenShiftDockerRegistryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []OpenShiftDockerRegistry `json:"items"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type OpenShiftDockerRegistry struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`
	Spec              OpenShiftDockerRegistrySpec   `json:"spec"`
	Status            OpenShiftDockerRegistryStatus `json:"status,omitempty"`
}

type OpenShiftDockerRegistryConfigProxy struct {
	HTTP  string `json:"http,omitempty"`
	HTTPS string `json:"https,omitempty"`
}

type OpenShiftDockerRegistryConfigStorageS3 struct {
	Bucket         string `json:"bucket,omitempty"`
	Region         string `json:"region,omitempty"`
	RegionEndpoint string `json:"regionEndpoint,omitempty"`
	Encrypt        bool   `json:"encrypt,omitempty"`
}

type OpenShiftDockerRegistryConfigStorageAzure struct {
	Container string `json:"container,omitempty"`
}

type OpenShiftDockerRegistryConfigStorageGCS struct {
	Bucket string `json:"bucket,omitempty"`
}

type OpenShiftDockerRegistryConfigStorageSwift struct {
	AuthURL   string `json:"authURL,omitempty"`
	Container string `json:"container,omitempty"`
}

type OpenShiftDockerRegistryConfigStorageFilesystem struct {
	VolumeSource corev1.VolumeSource `json:"volumeSource,omitempty"`
}

type OpenShiftDockerRegistryConfigStorage struct {
	Azure      *OpenShiftDockerRegistryConfigStorageAzure      `json:"azure,omitempty"`
	Filesystem *OpenShiftDockerRegistryConfigStorageFilesystem `json:"filesystem,omitempty"`
	GCS        *OpenShiftDockerRegistryConfigStorageGCS        `json:"gcs,omitempty"`
	S3         *OpenShiftDockerRegistryConfigStorageS3         `json:"s3,omitempty"`
	Swift      *OpenShiftDockerRegistryConfigStorageSwift      `json:"swift,omitempty"`
}

type OpenShiftDockerRegistryConfigRequestsLimits struct {
	MaxRunning     int           `json:"maxrunning,omitempty"`
	MaxInQueue     int           `json:"maxinqueue,omitempty"`
	MaxWaitInQueue time.Duration `json:"maxwaitinqueue,omitempty"`
}

type OpenShiftDockerRegistryConfigRequests struct {
	Read  OpenShiftDockerRegistryConfigRequestsLimits `json:"read,omitempty"`
	Write OpenShiftDockerRegistryConfigRequestsLimits `json:"write,omitempty"`
}

type OpenShiftDockerRegistryConfigRoute struct {
	Name       string `json:"name"`
	Hostname   string `json:"hostname"`
	SecretName string `json:"secretName"`
}

type OpenShiftDockerRegistrySpec struct {
	operatorsv1alpha1api.OperatorSpec `json:",inline"`

	HTTPSecret   string                                `json:"httpSecret,omitempty"`
	Proxy        OpenShiftDockerRegistryConfigProxy    `json:"proxy,omitempty"`
	Storage      OpenShiftDockerRegistryConfigStorage  `json:"storage,omitempty"`
	Requests     OpenShiftDockerRegistryConfigRequests `json:"requests,omitempty"`
	TLS          bool                                  `json:"tls,omitempty"`
	DefaultRoute bool                                  `json:"defaultRoute,omitempty"`
	Routes       []OpenShiftDockerRegistryConfigRoute  `json:"routes,omitempty"`
	NodeSelector map[string]string                     `json:"nodeSelector,omitempty"`
	Replicas     int32                                 `json:"replicas,omitempty"`
}
type OpenShiftDockerRegistryStatus struct {
	operatorsv1alpha1api.OperatorStatus `json:",inline"`
}
