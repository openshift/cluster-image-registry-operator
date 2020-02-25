package framework

import (
	"fmt"
	"reflect"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

func FlagExistsWithValue(args []string, flag string, value string) error {
	for _, arg := range args {
		if strings.HasPrefix(arg, flag) {
			if strings.Split(arg, "=")[1] == value {
				return nil
			}
			return fmt.Errorf("flag %q was found, but the value was %q when it should have been %q: %#v", flag, strings.Split(arg, "=")[1], value, args)
		}
	}
	return fmt.Errorf("flag %q was not found in %#v", flag, args)
}

func CheckEnvVars(want []corev1.EnvVar, have []corev1.EnvVar, includes bool) []error {
	var errs []error

	for _, val := range want {
		found := false
		for _, v := range have {
			if v.Name == val.Name {
				found = true
				if includes {
					if !strings.Contains(v.Value, val.Value) {
						errs = append(errs, fmt.Errorf("environment variable does not contain the expected value: expected %#v, got %#v", val, v))
					}
				} else {
					if !reflect.DeepEqual(v, val) {
						errs = append(errs, fmt.Errorf("environment variable does not equal the expected value: expected %#v, got %#v", val, v))
					}
				}
			}
		}
		if !found {
			errs = append(errs, fmt.Errorf("unable to find environment variable: wanted %s", val.Name))
		}
	}

	return errs
}
