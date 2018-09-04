package generate

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/operator-framework/operator-sdk/pkg/sdk"

	"k8s.io/apimachinery/pkg/api/errors"
	kmeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/client-go/util/retry"

	"github.com/openshift/cluster-image-registry-operator/pkg/parameters"
)

func checksum(o interface{}) (string, error) {
	data, err := json.Marshal(o)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data)), nil
}

func ApplyTemplate(tmpl Template, modified *bool) error {
	expected := tmpl.Expected()

	dgst, err := checksum(expected)
	if err != nil {
		return fmt.Errorf("unable to generate checksum for %s: %s", tmpl.Name(), err)
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := expected

		err := sdk.Get(current)
		if err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to get %s: %s", tmpl.Name(), err)
			}
			err = sdk.Create(expected)
			*modified = err == nil
			return err
		}

		if tmpl.Validator != nil {
			err = tmpl.Validator(current)
			if err != nil {
				return err
			}
		}

		currentMeta, err := kmeta.Accessor(current)
		if err != nil {
			return fmt.Errorf("unable to get meta accessor for current object: %s", err)
		}

		curdgst, ok := currentMeta.GetAnnotations()[parameters.ChecksumOperatorAnnotation]
		if ok && dgst == curdgst {
			return nil
		}

		updated, err := tmpl.Apply(current)
		if err != nil {
			return fmt.Errorf("unable to apply template %s: %s", tmpl.Name(), err)
		}

		updatedMeta, err := kmeta.Accessor(updated)
		if err != nil {
			return fmt.Errorf("unable to get meta accessor for updated object: %s", err)
		}

		if updatedMeta.GetAnnotations() == nil {
			updatedMeta.SetAnnotations(map[string]string{})
		}
		updatedMeta.GetAnnotations()[parameters.ChecksumOperatorAnnotation] = dgst

		err = sdk.Update(updated)
		*modified = err == nil
		return err
	})
}
