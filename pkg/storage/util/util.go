package util

import (
	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"

	coreapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"

	"github.com/openshift/cluster-image-registry-operator/pkg/client"
)

func CreateOrUpdateSecret(name string, namespace string, data map[string]string) error {
	kubeconfig, err := client.GetConfig()
	if err != nil {
		return err
	}

	client, err := coreset.NewForConfig(kubeconfig)
	if err != nil {
		return err
	}

	secretName := name + "-private-configuration"

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		cur, err := client.Secrets(namespace).Get(secretName, metaapi.GetOptions{})
		if err != nil {
			if !errors.IsNotFound(err) {
				return err
			}

			klog.Warningf("secret %s/%s not found: %s, creating", namespace, secretName, err)

			cur = &coreapi.Secret{
				ObjectMeta: metaapi.ObjectMeta{
					Name:      name + "-private-configuration",
					Namespace: namespace,
				},
			}
		}

		if cur.StringData == nil {
			cur.StringData = make(map[string]string)
		}
		for k, v := range data {
			cur.StringData[k] = v
		}

		if errors.IsNotFound(err) {
			_, err := client.Secrets(namespace).Create(cur)
			return err
		}
		_, err = client.Secrets(namespace).Update(cur)
		return err
	})
}

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
