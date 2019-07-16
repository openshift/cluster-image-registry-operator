package framework

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
)

func CreateOrUpdateSecret(name string, namespace string, data map[string]string) (*corev1.Secret, error) {
	kubeconfig, err := regopclient.GetConfig()
	if err != nil {
		return nil, err
	}

	client, err := coreset.NewForConfig(kubeconfig)
	if err != nil {
		return nil, err
	}
	var updatedSecret *corev1.Secret

	if err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Skip using the cache here so we don't have as many
		// retries due to slow cache updates
		cur, err := client.Secrets(namespace).Get(name, metav1.GetOptions{})
		if err != nil {
			if !errors.IsNotFound(err) {
				return err
			}

			klog.Warningf("secret %q not found: %s, creating", fmt.Sprintf("%s/%s", namespace, name), err)

			cur = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
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
		updatedSecret, err = client.Secrets(namespace).Update(cur)
		return err

	}); err != nil {
		return nil, err
	}

	return updatedSecret, err
}
