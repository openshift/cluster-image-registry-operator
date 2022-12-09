package resource

import (
	"reflect"
	"testing"

	rbacapi "k8s.io/api/rbac/v1"
)

func TestImageRules(t *testing.T) {
	generator := newGeneratorClusterRole(nil, nil)
	expected := rbacapi.PolicyRule{
		Verbs:     []string{"get", "update", "create"},
		APIGroups: []string{"image.openshift.io"},
		Resources: []string{"images"},
	}
	r, err := generator.expected()
	if err != nil {
		t.Fatalf("error getting desired cluster role: %#v", err)
	}
	role, ok := r.(*rbacapi.ClusterRole)
	if !ok {
		t.Fatal("failed to cast object to ClusterRole")
	}

	for _, rule := range role.Rules {
		if !reflect.DeepEqual(rule.Resources, expected.Resources) {
			continue
		}
		if !reflect.DeepEqual(rule.Verbs, expected.Verbs) {
			t.Error("images rule.Verbs differ")
			t.Errorf("want %#v", expected.Verbs)
			t.Errorf("got  %#v", rule.Verbs)
		}
	}
}
