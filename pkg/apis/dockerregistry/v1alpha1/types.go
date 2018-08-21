package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

type OpenShiftDockerRegistrySpec struct {
	// Fill me
}
type OpenShiftDockerRegistryStatus struct {
	// Fill me
}
