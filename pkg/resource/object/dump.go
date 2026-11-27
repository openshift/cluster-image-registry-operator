package object

import (
	"fmt"
	"sort"
	"strings"

	corev1 "k8s.io/api/core/v1"
)

func printValue(obj interface{}, key string, value string) string {
	switch obj.(type) {
	case *corev1.Secret:
		if value != "" && (strings.HasPrefix(key, "data.") || strings.HasPrefix(key, "stringData.")) {
			return "<REDACTED>"
		}
	case *corev1.ConfigMap:
		// ConfigMap data can contain certificate bundles (e.g. the
		// image-registry-certificates ConfigMap); don't print it verbatim.
		if value != "" && (strings.HasPrefix(key, "data.") || strings.HasPrefix(key, "binaryData.")) {
			return "<REDACTED>"
		}
	}
	return fmt.Sprintf("%q", value)
}

func DumpString(o interface{}) (string, error) {
	res, err := convertToMap(o)
	if err != nil {
		return "", fmt.Errorf("unable to convert to map the object: %s", err)
	}

	keys := make([]string, len(res))
	i := 0
	for k := range res {
		keys[i] = k
		i += 1
	}
	sort.Strings(keys)

	sep := ""
	s := ""

	for _, k := range keys {
		s += fmt.Sprintf("%s%s=%s", sep, k, printValue(o, k, res[k]))
		sep = ", "
	}

	return s, nil
}
