package envvar

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v2"

	corev1 "k8s.io/api/core/v1"
)

// EnvVar represents a value for a Distribution configuration parameter.
type EnvVar struct {
	// Name is the environment name for the parameter.
	Name string
	// Value is the value of the parameter.
	Value interface{}
	// Secret indicates whether to the value contains sensitive information and
	// should be stored in a secret.
	Secret bool
}

// EnvValue returns the string that represents Value and can be used a value
// for the environment variable.
func (e EnvVar) EnvValue() (string, error) {
	buf, err := yaml.Marshal(e.Value)
	if err != nil {
		return "", fmt.Errorf("unable to encode environment variable %s: %s", e.Name, err)
	}
	return strings.TrimRight(string(buf), "\n"), err
}

// List is a list of configuration parameters.
type List []EnvVar

// EnvVars returns a list of environment variables to set in the container.
// Secret values are sourced from the secret.
func (l List) EnvVars(secretName string) ([]corev1.EnvVar, error) {
	var envvars []corev1.EnvVar
	for _, e := range l {
		envvar := corev1.EnvVar{
			Name: e.Name,
		}
		if e.Secret {
			envvar.ValueFrom = &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: secretName,
					},
					Key: e.Name,
				},
			}
		} else {
			value, err := e.EnvValue()
			if err != nil {
				return nil, err
			}
			envvar.Value = value
		}
		envvars = append(envvars, envvar)
	}
	return envvars, nil
}

// SecretData returns a data for the secret that should be used with the
// EnvVars method.
func (l List) SecretData() (map[string]string, error) {
	data := make(map[string]string)
	for _, e := range l {
		if !e.Secret {
			continue
		}

		value, err := e.EnvValue()
		if err != nil {
			return nil, err
		}
		data[e.Name] = value
	}
	return data, nil
}
