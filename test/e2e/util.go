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
	kappsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	regopset "github.com/openshift/cluster-image-registry-operator/pkg/generated/clientset/versioned/typed/imageregistry/v1alpha1"
	kappsset "k8s.io/client-go/kubernetes/typed/apps/v1"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/client"
)

func getImageRegistryPrivateConfiguration() (*corev1.Secret, error) {
	kubeconfig, err := client.GetConfig()
	if err != nil {
		return nil, err
	}

	client, err := coreset.NewForConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	sec, err := client.Secrets("openshift-image-registry").Get("image-registry-private-configuration", metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("unable to read secret openshift-image-registry/image-registry-private-configuration: %v", err)
	}

	return sec, nil
}

func getImageRegistryCustomResource() (*regopapi.ImageRegistry, error) {
	kubeconfig, err := client.GetConfig()
	if err != nil {
		return nil, err
	}

	client, err := regopset.NewForConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	return client.ImageRegistries().Get(registryCustomResourceName, metav1.GetOptions{})
}

func getRegistryDeployment() (*kappsv1.Deployment, error) {
	kubeconfig, err := client.GetConfig()
	if err != nil {
		return nil, err
	}

	client, err := kappsset.NewForConfig(kubeconfig)
	if err != nil {
		return nil, err
	}

	return client.Deployments("openshift-image-registry").Get("image-registry", metav1.GetOptions{})
}
