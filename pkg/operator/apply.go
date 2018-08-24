package operator

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"

	"github.com/operator-framework/operator-sdk/pkg/sdk"

	appsapi "github.com/openshift/api/apps/v1"
	authapi "github.com/openshift/api/authorization/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

func checksum(o interface{}) (string, error) {
	data, err := json.Marshal(o)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data)), nil
}

func mergeObjectMeta(existing, required *metav1.ObjectMeta) {
	existing.Name = required.Name
	existing.Namespace = required.Namespace
	existing.Labels = required.Labels
	existing.Annotations = required.Annotations
	existing.OwnerReferences = required.OwnerReferences
}

func ApplyServiceAccount(expect *corev1.ServiceAccount, modified *bool) error {
	dgst, err := checksum(expect)
	if err != nil {
		return fmt.Errorf("unable to generate CR checksum: %s", err)
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &corev1.ServiceAccount{
			TypeMeta:   expect.TypeMeta,
			ObjectMeta: expect.ObjectMeta,
		}

		err := sdk.Get(current)
		if err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to get service account %s: %v", expect.GetName(), err)
			}
			err = sdk.Create(expect)
			*modified = err == nil
			return err
		}

		curdgst, ok := current.ObjectMeta.Annotations[checksumOperatorAnnotation]
		if ok && dgst == curdgst {
			return nil
		}

		if expect.ObjectMeta.Annotations == nil {
			expect.ObjectMeta.Annotations = map[string]string{}
		}
		expect.ObjectMeta.Annotations[checksumOperatorAnnotation] = dgst

		mergeObjectMeta(&current.ObjectMeta, &expect.ObjectMeta)
		current.Secrets = expect.Secrets
		current.ImagePullSecrets = expect.ImagePullSecrets
		current.AutomountServiceAccountToken = expect.AutomountServiceAccountToken

		err = sdk.Update(current)
		*modified = err == nil
		return err
	})
}

func ApplyClusterRole(expect *authapi.ClusterRole, modified *bool) error {
	dgst, err := checksum(expect)
	if err != nil {
		return fmt.Errorf("unable to generate CR checksum: %s", err)
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &authapi.ClusterRole{
			TypeMeta:   expect.TypeMeta,
			ObjectMeta: expect.ObjectMeta,
		}

		err := sdk.Get(current)
		if err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to get cluster role %s: %v", expect.GetName(), err)
			}
			*modified = err == nil
			return sdk.Create(expect)
		}

		curdgst, ok := current.ObjectMeta.Annotations[checksumOperatorAnnotation]
		if ok && dgst == curdgst {
			return nil
		}

		if expect.ObjectMeta.Annotations == nil {
			expect.ObjectMeta.Annotations = map[string]string{}
		}
		expect.ObjectMeta.Annotations[checksumOperatorAnnotation] = dgst

		mergeObjectMeta(&current.ObjectMeta, &expect.ObjectMeta)
		current.Rules = expect.Rules
		current.AggregationRule = expect.AggregationRule

		err = sdk.Update(current)
		*modified = err == nil
		return err
	})
}

func ApplyClusterRoleBinding(expect *authapi.ClusterRoleBinding, modified *bool) error {
	dgst, err := checksum(expect)
	if err != nil {
		return fmt.Errorf("unable to generate CR checksum: %s", err)
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &authapi.ClusterRoleBinding{
			TypeMeta:   expect.TypeMeta,
			ObjectMeta: expect.ObjectMeta,
		}

		err := sdk.Get(current)
		if err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to get deployment config %s: %v", expect.GetName(), err)
			}
			err = sdk.Create(expect)
			*modified = err == nil
			return err
		}

		curdgst, ok := current.ObjectMeta.Annotations[checksumOperatorAnnotation]
		if ok && dgst == curdgst {
			return nil
		}

		if expect.ObjectMeta.Annotations == nil {
			expect.ObjectMeta.Annotations = map[string]string{}
		}
		expect.ObjectMeta.Annotations[checksumOperatorAnnotation] = dgst

		mergeObjectMeta(&current.ObjectMeta, &expect.ObjectMeta)
		current.Subjects = expect.Subjects
		current.RoleRef = expect.RoleRef

		err = sdk.Update(current)
		*modified = err == nil
		return err
	})
}

func ApplyService(expect *corev1.Service, modified *bool) error {
	dgst, err := checksum(expect)
	if err != nil {
		return fmt.Errorf("unable to generate CR checksum: %s", err)
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &corev1.Service{
			TypeMeta:   expect.TypeMeta,
			ObjectMeta: expect.ObjectMeta,
		}

		err := sdk.Get(current)
		if err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to get service %s: %v", expect.GetName(), err)
			}
			err = sdk.Create(expect)
			*modified = err == nil
			return err
		}

		curdgst, ok := current.ObjectMeta.Annotations[checksumOperatorAnnotation]
		if ok && dgst == curdgst {
			return nil
		}

		if expect.ObjectMeta.Annotations == nil {
			expect.ObjectMeta.Annotations = map[string]string{}
		}
		expect.ObjectMeta.Annotations[checksumOperatorAnnotation] = dgst

		mergeObjectMeta(&current.ObjectMeta, &expect.ObjectMeta)
		current.Spec.Selector = expect.Spec.Selector
		current.Spec.Type = expect.Spec.Type
		current.Spec.Ports = expect.Spec.Ports

		err = sdk.Update(current)
		*modified = err == nil
		return err
	})
}

func ApplyDeploymentConfig(expect *appsapi.DeploymentConfig, modified *bool) error {
	dgst, err := checksum(expect)
	if err != nil {
		return fmt.Errorf("unable to generate CR checksum: %s", err)
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		current := &appsapi.DeploymentConfig{
			TypeMeta:   expect.TypeMeta,
			ObjectMeta: expect.ObjectMeta,
		}

		err := sdk.Get(current)
		if err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to get deployment config %s: %v", expect.GetName(), err)
			}
			err = sdk.Create(expect)
			*modified = err == nil
			return err
		}

		curdgst, ok := current.ObjectMeta.Annotations[checksumOperatorAnnotation]
		if ok && dgst == curdgst {
			return nil
		}

		if expect.ObjectMeta.Annotations == nil {
			expect.ObjectMeta.Annotations = map[string]string{}
		}
		expect.ObjectMeta.Annotations[checksumOperatorAnnotation] = dgst

		mergeObjectMeta(&current.ObjectMeta, &expect.ObjectMeta)
		current.Spec = expect.Spec

		err = sdk.Update(current)
		*modified = err == nil
		return err
	})
}
