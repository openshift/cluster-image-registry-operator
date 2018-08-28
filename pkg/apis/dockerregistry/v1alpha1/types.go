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
	HTTP  string
	HTTPS string
}

type OpenShiftDockerRegistryConfigStorageS3 struct {
	AccessKey      string
	SecretKey      string
	Bucket         string
	Region         string
	RegionEndpoint string
	Encrypt        bool
}

type OpenShiftDockerRegistryConfigStorageAzure struct {
	AccountName string
	AccountKey  string
	Container   string
}

type OpenShiftDockerRegistryConfigStorageGCS struct {
	Bucket string
}

type OpenShiftDockerRegistryConfigStorageSwift struct {
	AuthURL   string
	Username  string
	Password  string
	Container string
}

type OpenShiftDockerRegistryConfigStorageFilesystem struct {
	VolumeSource corev1.VolumeSource `json:"volumeSource"`
}

type OpenShiftDockerRegistryConfigStorage struct {
	Azure      *OpenShiftDockerRegistryConfigStorageAzure      `json:"azure"`
	Filesystem *OpenShiftDockerRegistryConfigStorageFilesystem `json:"filesystem"`
	GCS        *OpenShiftDockerRegistryConfigStorageGCS        `json:"gcs"`
	S3         *OpenShiftDockerRegistryConfigStorageS3         `json:"s3"`
	Swift      *OpenShiftDockerRegistryConfigStorageSwift      `json:"swift"`
}

type OpenShiftDockerRegistryConfigRequestsLimits struct {
	MaxRunning     int           `json:"maxrunning"`
	MaxInQueue     int           `json:"maxinqueue"`
	MaxWaitInQueue time.Duration `json:"maxwaitinqueue"`
}

type OpenShiftDockerRegistryConfigRequests struct {
	Read  OpenShiftDockerRegistryConfigRequestsLimits `json:"read"`
	Write OpenShiftDockerRegistryConfigRequestsLimits `json:"write"`
}

type OpenShiftDockerRegistryConfigTLSCertificate struct {
	SecretKeyRef *corev1.SecretKeySelector `json:"secretKeyRef"`
}

type OpenShiftDockerRegistryConfigTLSKey struct {
	SecretKeyRef *corev1.SecretKeySelector `json:"secretKeyRef"`
}

type OpenShiftDockerRegistryConfigTLS struct {
	Certificate OpenShiftDockerRegistryConfigTLSCertificate `json:"certificate"`
	Key         OpenShiftDockerRegistryConfigTLSKey         `json:"key"`
}

type OpenShiftDockerRegistryConfigRoute struct {
	Hostname string `json:"hostname"`
}

type OpenShiftDockerRegistrySpec struct {
	operatorsv1alpha1api.OperatorSpec `json:",inline"`

	HTTPSecret   string                                `json:"HTTPSecret"`
	Proxy        OpenShiftDockerRegistryConfigProxy    `json:"proxy"`
	Storage      OpenShiftDockerRegistryConfigStorage  `json:"storage"`
	Requests     OpenShiftDockerRegistryConfigRequests `json:"requests"`
	TLS          OpenShiftDockerRegistryConfigTLS      `json:"tls"`
	CAs          string                                `json:"CAs"`
	Route        OpenShiftDockerRegistryConfigRoute    `json:"route"`
	NodeSelector map[string]string                     `json:"nodeSelector"`
	Replicas     int32                                 `json:"replicas"`
}
type OpenShiftDockerRegistryStatus struct {
	operatorsv1alpha1api.OperatorStatus `json:",inline"`
}
