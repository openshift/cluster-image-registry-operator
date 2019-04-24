package object

import (
	"fmt"
	"sort"
)

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
		s += fmt.Sprintf("%s%s=%q", sep, k, res[k])
		sep = ", "
	}

	return s, nil
}
