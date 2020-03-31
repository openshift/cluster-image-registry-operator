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

func CheckEnvVarsAreNotSet(te TestEnv, got []corev1.EnvVar, names []string) {
	blacklist := map[string]bool{}
	for _, name := range names {
		blacklist[name] = true
	}

	for _, e := range got {
		if blacklist[e.Name] {
			te.Errorf("got the environment variable %s with the value %q, but want it to be absent", e.Name, e.Value)
		}
	}
}

func CheckEnvVars(te TestEnv, want []corev1.EnvVar, have []corev1.EnvVar, includes bool) {
	for _, val := range want {
		found := false
		for _, v := range have {
			if v.Name == val.Name {
				found = true
				if includes {
					if !strings.Contains(v.Value, val.Value) {
						te.Errorf("environment variable does not contain the expected value: expected %#v, got %#v", val, v)
					}
				} else {
					if !reflect.DeepEqual(v, val) {
						te.Errorf("environment variable does not equal the expected value: expected %#v, got %#v", val, v)
					}
				}
			}
		}
		if !found {
			te.Errorf("unable to find environment variable: wanted %s", val.Name)
		}
	}
}
