package util

import (
	"github.com/operator-framework/operator-sdk/pkg/sdk"

	"github.com/sirupsen/logrus"

	opapi "github.com/openshift/cluster-image-registry-operator/pkg/apis/imageregistry/v1alpha1"

	coreapi "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

func CreateOrUpdateSecret(name string, namespace string, data map[string]string) error {
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		cur := &coreapi.Secret{
			TypeMeta: metaapi.TypeMeta{
				APIVersion: coreapi.SchemeGroupVersion.String(),
				Kind:       "Secret",
			},
			ObjectMeta: metaapi.ObjectMeta{
				Name:      name + "-private-configuration",
				Namespace: namespace,
			},
		}
		err := sdk.Get(cur)
		if err != nil && !errors.IsNotFound(err) {
			logrus.Warnf("failed to get secret %s: %s, creating", cur.Name, err)
		}
		if cur.StringData == nil {
			cur.StringData = make(map[string]string)
		}
		for k, v := range data {
			cur.StringData[k] = v
		}

		if errors.IsNotFound(err) {
			return sdk.Create(cur)
		}
		return sdk.Update(cur)
	})
	if err != nil {
		return err
	}
	return nil
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
