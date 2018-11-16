package resource

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/operator-framework/operator-sdk/pkg/sdk"

	"k8s.io/apimachinery/pkg/api/errors"
	kmeta "k8s.io/apimachinery/pkg/api/meta"
	metaapi "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

func Checksum(o interface{}) (string, error) {
	data, err := json.Marshal(o)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data)), nil
}

func ApplyTemplate(tmpl Template, force bool, modified *bool) error {
	dgst, err := Checksum(tmpl.Expected())
	if err != nil {
		return fmt.Errorf("unable to generate checksum for %s: %s", tmpl.Name(), err)
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := tmpl.Expected()

		err := sdk.Get(current)
		if err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to get object %s: %s", tmpl.Name(), err)
			}

			logrus.Infof("creating object: %s", tmpl.Name())

			err = sdk.Create(current)
			if err == nil {
				*modified = true
				return nil
			}
			return fmt.Errorf("failed to create object %s: %s", tmpl.Name(), err)
		}

		if tmpl.Validator != nil {
			err = tmpl.Validator(current)
			if err != nil {
				return err
			}
		}

		currentMeta, err := kmeta.Accessor(current)
		if err != nil {
			return fmt.Errorf("unable to get meta accessor for current object %s: %s", tmpl.Name(), err)
		}

		curdgst, ok := currentMeta.GetAnnotations()[parameters.ChecksumOperatorAnnotation]
		if !force && ok && dgst == curdgst {
			logrus.Debugf("object has not changed: %s", tmpl.Name())
			return nil
		}

		updated, err := tmpl.Apply(current)
		if err != nil {
			return fmt.Errorf("unable to apply template %s: %s", tmpl.Name(), err)
		}

		updatedMeta, err := kmeta.Accessor(updated)
		if err != nil {
			return fmt.Errorf("unable to get meta accessor for updated object %s: %s", tmpl.Name(), err)
		}

		if updatedMeta.GetAnnotations() == nil {
			if tmpl.Annotations != nil {
				updatedMeta.SetAnnotations(tmpl.Annotations)
			} else {
				updatedMeta.SetAnnotations(map[string]string{})
			}
		}
		updatedMeta.GetAnnotations()[parameters.ChecksumOperatorAnnotation] = dgst

		if force {
			updatedMeta.SetGeneration(currentMeta.GetGeneration() + 1)
		}

		logrus.Infof("updating object: %s", tmpl.Name())

		err = sdk.Update(updated)
		if err == nil {
			*modified = true
			return nil
		}
		return fmt.Errorf("failed to update object %s: %s", tmpl.Name(), err)
	})
}

func RemoveByTemplate(tmpl Template, modified *bool) error {
	gracePeriod := int64(0)
	propagationPolicy := metaapi.DeletePropagationForeground

	opt := sdk.WithDeleteOptions(&metaapi.DeleteOptions{
		GracePeriodSeconds: &gracePeriod,
		PropagationPolicy:  &propagationPolicy,
	})

	logrus.Infof("deleting opject %s", tmpl.Name())

	err := sdk.Delete(tmpl.Expected(), opt)
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to delete %s: %s", tmpl.Name(), err)
		}
		return nil
	}
	*modified = true
	return nil
}
