package util

import (
	"fmt"

	"github.com/golang/glog"

	coreapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreset "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/util/retry"

	regopclient "github.com/openshift/cluster-image-registry-operator/pkg/client"
)

func CreateOrUpdateSecret(name string, namespace string, data map[string]string) error {
	kubeconfig, err := regopclient.GetConfig()
	if err != nil {
		return err
	}

	client, err := coreset.NewForConfig(kubeconfig)
	if err != nil {
		return err
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		cur, err := client.Secrets(namespace).Get(name, metaapi.GetOptions{})
		if err != nil {
			if !errors.IsNotFound(err) {
				return err
			}

			glog.Warningf("secret %q not found: %s, creating", fmt.Sprintf("%s/%s", namespace, name), err)

			cur = &coreapi.Secret{
				ObjectMeta: metaapi.ObjectMeta{
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
		_, err = client.Secrets(namespace).Update(cur)
		return err
	})
}
