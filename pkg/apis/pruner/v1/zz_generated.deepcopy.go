// +build !ignore_autogenerated

// Code generated by deepcopy-gen. DO NOT EDIT.

package v1

import (
	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *Config) DeepCopyInto(out *Config) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Spec.DeepCopyInto(&out.Spec)
	in.Status.DeepCopyInto(&out.Status)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new Config.
func (in *Config) DeepCopy() *Config {
	if in == nil {
		return nil
	}
	out := new(Config)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *Config) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *ConfigList) DeepCopyInto(out *ConfigList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]Config, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new ConfigList.
func (in *ConfigList) DeepCopy() *ConfigList {
	if in == nil {
		return nil
	}
	out := new(ConfigList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject is an autogenerated deepcopy function, copying the receiver, creating a new runtime.Object.
func (in *ConfigList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PrunerHistory) DeepCopyInto(out *PrunerHistory) {
	*out = *in
	if in.SuccessfulJobsHistoryLimit != nil {
		in, out := &in.SuccessfulJobsHistoryLimit, &out.SuccessfulJobsHistoryLimit
		*out = new(int32)
		**out = **in
	}
	if in.FailedJobsHistoryLimit != nil {
		in, out := &in.FailedJobsHistoryLimit, &out.FailedJobsHistoryLimit
		*out = new(int32)
		**out = **in
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PrunerHistory.
func (in *PrunerHistory) DeepCopy() *PrunerHistory {
	if in == nil {
		return nil
	}
	out := new(PrunerHistory)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PrunerSpec) DeepCopyInto(out *PrunerSpec) {
	*out = *in
	if in.Suspend != nil {
		in, out := &in.Suspend, &out.Suspend
		*out = new(bool)
		**out = **in
	}
	if in.KeepTagRevisions != nil {
		in, out := &in.KeepTagRevisions, &out.KeepTagRevisions
		*out = new(int)
		**out = **in
	}
	if in.Resources != nil {
		in, out := &in.Resources, &out.Resources
		*out = new(corev1.ResourceRequirements)
		(*in).DeepCopyInto(*out)
	}
	if in.Affinity != nil {
		in, out := &in.Affinity, &out.Affinity
		*out = new(corev1.NodeAffinity)
		(*in).DeepCopyInto(*out)
	}
	if in.NodeSelector != nil {
		in, out := &in.NodeSelector, &out.NodeSelector
		*out = make(map[string]string, len(*in))
		for key, val := range *in {
			(*out)[key] = val
		}
	}
	if in.Tolerations != nil {
		in, out := &in.Tolerations, &out.Tolerations
		*out = make([]corev1.Toleration, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	if in.StartingDeadlineSeconds != nil {
		in, out := &in.StartingDeadlineSeconds, &out.StartingDeadlineSeconds
		*out = new(int64)
		**out = **in
	}
	in.History.DeepCopyInto(&out.History)
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PrunerSpec.
func (in *PrunerSpec) DeepCopy() *PrunerSpec {
	if in == nil {
		return nil
	}
	out := new(PrunerSpec)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto is an autogenerated deepcopy function, copying the receiver, writing into out. in must be non-nil.
func (in *PrunerStatus) DeepCopyInto(out *PrunerStatus) {
	*out = *in
	if in.Conditions != nil {
		in, out := &in.Conditions, &out.Conditions
		*out = make([]operatorv1.OperatorCondition, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
	return
}

// DeepCopy is an autogenerated deepcopy function, copying the receiver, creating a new PrunerStatus.
func (in *PrunerStatus) DeepCopy() *PrunerStatus {
	if in == nil {
		return nil
	}
	out := new(PrunerStatus)
	in.DeepCopyInto(out)
	return out
}
