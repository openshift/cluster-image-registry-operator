package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorsapiv1 "github.com/openshift/api/operator/v1"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

type ConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []Config `json:"items"`
}

// +genclient
// +genclient:nonNamespaced
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// Config is the configuration object for an image registry pruner
type Config struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec PrunerSpec `json:"spec"`
	// +optional
	Status PrunerStatus `json:"status"`
}

// PrunerHistory is a configuration object for how many
// successful and failed finished jobs to retain
type PrunerHistory struct {
	// SuccessfulJobsHistoryLimit specifies how many successful finished jobs to retain
	// Defaults to 3 if not set
	// +optional
	SuccessfulJobsHistoryLimit *int32 `json:"successfulJobsHistoryLimit"`
	// FailedJobsHistoryLimit specifies how many failed finished jobs to retain
	// Defaults to 3 if not set
	// +optional
	FailedJobsHistoryLimit *int32 `json:"failedJobsHistoryLimit"`
}

type PrunerSpec struct {
	// Schedule specifies when to execute the job
	// Uses standard cronjob syntax: https://wikipedia.org/wiki/Cron
	// Defaults to 0 0 * * *
	// +optional
	Schedule string `json:"schedule"`
	// Suspend specifies wether or not to suspend subsequent executions of this cronjob
	// Defaults to false
	// +required
	Suspend *bool `json:"suspend"`
	// KeepTagRevisions specifies how many tag revisions to keep
	// Defaults to 5
	// +optional
	KeepTagRevisions *int `json:"keepTagRevisions"`
	// KeepYoungerThan specifies how old an image needs to be for it to be pruned
	// Defaults to 96h (96 hours)
	// +optional
	KeepYoungerThan string `json:"keepYoungerThan"`
	// Resources defines the resource requests+limits for the registry pod.
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	// Affinity is a group of node affinity scheduling rules.
	// +optional
	Affinity *corev1.NodeAffinity `json:"affinity,omitempty"`
	// NodeSelector defines the node selection constraints for the registry
	// pod.
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// Tolerations defines the tolerations for the registry pod.
	// +optional
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
	// StartingDeadlineSeconds specifies how long of a grace period in seconds
	// to give the job to start if it misses it's scheduled time
	// Defaults to 0
	// +optional
	StartingDeadlineSeconds *int64 `json:"startingDeadlineSeconds,omitempty"`
	// History specifies how many successful and failed finished jobs to retain
	// +optional
	History PrunerHistory `json:"history,omitempty"`
}

type PrunerStatus struct {
	// ObservedGeneration is the last generation change you've dealt with
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions is a list of conditions and their status
	// +optional
	Conditions []operatorsapiv1.OperatorCondition `json:"conditions,omitempty"`
}
