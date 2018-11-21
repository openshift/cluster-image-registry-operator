// Copyright 2018 The Operator-SDK Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package e2e

import (
	"fmt"

	regopapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"
	"github.com/operator-framework/operator-sdk/pkg/sdk"
	kappsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func getImageRegistryPrivateConfiguration() (*corev1.Secret, error) {
	sec := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "image-registry-private-configuration",
			Namespace: "openshift-image-registry",
		},
	}

	if err := sdk.Get(sec); err != nil {
		return nil, fmt.Errorf("unable to read secret openshift-image-registry/image-registry-private-configuration: %v", err)
	}

	return sec, nil
}

func getImageRegistryCustomResource() (*regopapi.ImageRegistry, error) {
	cr := &regopapi.ImageRegistry{
		TypeMeta: metav1.TypeMeta{
			APIVersion: regopapi.SchemeGroupVersion.String(),
			Kind:       "ImageRegistry",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      registryCustomResourceName,
			Namespace: registryNamespace,
		},
	}

	err := sdk.Get(cr)
	if err != nil {
		return nil, err
	}
	return cr, nil
}

func getRegistryDeployment() (*kappsv1.Deployment, error) {
	rd := &kappsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: kappsv1.SchemeGroupVersion.String(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "image-registry",
			Namespace: "openshift-image-registry",
		},
	}

	err := sdk.Get(rd)
	if err != nil {
		return nil, err
	}
	return rd, nil
}
