package util

import (
	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
)

const (
	STORAGE_PREFIX = "image-registry"
)

func GetStateValue(status *opapi.ImageRegistryStatus, name string) (value string, found bool) {
	for _, elem := range status.State {
		if elem.Name == name {
			value = elem.Value
			found = true
			break
		}
	}
	return
}

func SetStateValue(status *opapi.ImageRegistryStatus, name, value string) {
	for i, elem := range status.State {
		if elem.Name == name {
			status.State[i].Value = value
			return
		}
	}
	status.State = append(status.State, opapi.ImageRegistryStatusState{
		Name:  name,
		Value: value,
	})
}
