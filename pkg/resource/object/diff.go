package object

import (
	"encoding/json"
	"fmt"
	"sort"

	"k8s.io/klog"
)

func printDiff(oldv, newv map[string]string, printer func(key, typ, oldv, newv string)) {
	diff := make(map[string][]string)

	for k, v := range newv {
		if _, ok := oldv[k]; !ok {
			diff[k] = []string{"n", "", v}
		}
	}
	for k, v := range oldv {
		if _, ok := newv[k]; !ok {
			diff[k] = []string{"o", v, ""}
		} else if v != newv[k] {
			diff[k] = []string{"c", v, newv[k]}
		}
	}

	if len(diff) == 0 {
		return
	}

	keys := make([]string, len(diff))
	i := 0
	for k := range diff {
		keys[i] = k
		i += 1
	}
	sort.Strings(keys)

	for _, k := range keys {
		printer(k, diff[k][0], diff[k][1], diff[k][2])
	}

	return
}

func pairs(prefix string, o interface{}) (res map[string]string) {
	res = map[string]string{}
	switch t := o.(type) {
	case nil:
		res[prefix] = "nil"
	case bool:
		res[prefix] = fmt.Sprintf("%v", t)
	case string:
		res[prefix] = t
	case int, int32, int64:
		res[prefix] = fmt.Sprintf("%d", t)
	case float32, float64:
		res[prefix] = fmt.Sprintf("%f", t)
	case map[string]interface{}:
		if len(prefix) > 0 {
			prefix += "."
		}
		for k, v := range t {
			for a, b := range pairs(prefix+k, v) {
				res[a] = b
			}
		}
	case []interface{}:
		if len(prefix) > 0 {
			prefix += "."
		}
		for i, e := range t {
			for a, b := range pairs(fmt.Sprintf("%s%d", prefix, i), e) {
				res[a] = b
			}
		}
	default:
		klog.Errorf("unknown %T\n", t)
	}
	return
}

func convertToMap(o interface{}) (res map[string]string, err error) {
	var b []byte
	b, err = json.Marshal(o)
	if err != nil {
		return
	}

	out := make(map[string]interface{})
	err = json.Unmarshal(b, &out)
	if err != nil {
		return
	}

	res = pairs("", out)

	return
}

func DiffString(old_o, new_o interface{}) (string, error) {
	res0, err := convertToMap(old_o)
	if err != nil {
		return "", fmt.Errorf("unable to convert to map the old object: %s", err)
	}

	res1, err := convertToMap(new_o)
	if err != nil {
		return "", fmt.Errorf("unable to convert to map the new object: %s", err)
	}

	sep := ""
	s := ""
	printDiff(res0, res1, func(key, typ, oldv, newv string) {
		switch typ {
		case "n":
			s += fmt.Sprintf("%sadded:%s=%q", sep, key, newv)
		case "o":
			s += fmt.Sprintf("%sremoved:%s=%q", sep, key, oldv)
		case "c":
			s += fmt.Sprintf("%schanged:%s={%q -> %q}", sep, key, oldv, newv)
		}
		sep = ", "
	})

	return s, nil
}
