package framework

import (
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
)

func CheckEnvVars(want []corev1.EnvVar, have []corev1.EnvVar) []error {
	var errs []error

	for _, val := range want {
		found := false
		for _, v := range have {
			if v.Name == val.Name {
				found = true
				if !reflect.DeepEqual(v, val) {
					errs = append(errs, fmt.Errorf("environment variable contains incorrect data: expected %#v, got %#v", val, v))
				}
			}
		}
		if !found {
			errs = append(errs, fmt.Errorf("unable to find environment variable: wanted %s", val.Name))
		}
	}

	return errs
}
