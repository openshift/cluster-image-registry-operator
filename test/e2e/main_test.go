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
	"os"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	appsset "k8s.io/client-go/kubernetes/typed/apps/v1"

	"github.com/openshift/cluster-image-registry-operator/pkg/client"
)

func TestMain(m *testing.M) {
	// e2e test job does not guarantee our operator is up before
	// launching the test, so we need to do so.
	err := waitForOperator()
	if err != nil {
		fmt.Println("failed waiting for operator to start")
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func waitForOperator() error {
	kubeconfig, err := client.GetConfig()
	if err != nil {
		return err
	}

	appsclient, err := appsset.NewForConfig(kubeconfig)
	if err != nil {
		return err
	}

	err = wait.PollImmediate(1*time.Second, 10*time.Minute, func() (bool, error) {
		_, err := appsclient.Deployments("openshift-image-registry").Get("cluster-image-registry-operator", metav1.GetOptions{})
		if err != nil {
			fmt.Printf("error waiting for operator deployment to exist: %v\n", err)
			return false, nil
		}
		return true, nil
	})
	return err
}
