package auth

import (
	"reflect"
	"testing"

	"github.com/docker/distribution/context"
	"github.com/docker/distribution/registry/auth"
)

func checkResolveScopeSpecifiers(t *testing.T, scopes []string, expectedAuth []auth.Access) {
	res := ResolveScopeSpecifiers(context.Background(), scopes)
	if !reflect.DeepEqual(res, expectedAuth) {
		t.Fatalf("%q: expected: %#v, got: %#v", scopes, expectedAuth, res)
	}
}

func TestSimpleScope(t *testing.T) {
	checkResolveScopeSpecifiers(t, []string{"repository:namespace/name:pull"}, []auth.Access{
		{
			Resource: auth.Resource{
				Type:  "repository",
				Class: "",
				Name:  "namespace/name",
			},
			Action: "pull",
		},
	})
	checkResolveScopeSpecifiers(t, []string{"repository:namespace/name:pull,push,pull"}, []auth.Access{
		{
			Resource: auth.Resource{
				Type:  "repository",
				Class: "",
				Name:  "namespace/name",
			},
			Action: "pull",
		},
		{
			Resource: auth.Resource{
				Type:  "repository",
				Class: "",
				Name:  "namespace/name",
			},
			Action: "push",
		},
	})
	checkResolveScopeSpecifiers(t, []string{"repository:namespace/name:"}, []auth.Access{
		{
			Resource: auth.Resource{
				Type:  "repository",
				Class: "",
				Name:  "namespace/name",
			},
			Action: "",
		},
	})
	checkResolveScopeSpecifiers(t, []string{"repository::"}, []auth.Access{
		{
			Resource: auth.Resource{
				Type:  "repository",
				Class: "",
				Name:  "",
			},
			Action: "",
		},
	})
	checkResolveScopeSpecifiers(t, []string{"::"}, []auth.Access{})
}

func TestScopeWithClass(t *testing.T) {
	checkResolveScopeSpecifiers(t, []string{"repository(plugin):namespace/name:pull"}, []auth.Access{
		{
			Resource: auth.Resource{
				Type:  "repository",
				Class: "plugin",
				Name:  "namespace/name",
			},
			Action: "pull",
		},
	})
}

func TestMultiScopes(t *testing.T) {
	checkResolveScopeSpecifiers(t,
		[]string{
			"repository(plugin):namespace/name:pull",
			"repository(plugin):namespace/name:push",
			"repository(plugin):namespace/name:pull",
		},
		[]auth.Access{
			{
				Resource: auth.Resource{
					Type:  "repository",
					Class: "plugin",
					Name:  "namespace/name",
				},
				Action: "pull",
			},
			{
				Resource: auth.Resource{
					Type:  "repository",
					Class: "plugin",
					Name:  "namespace/name",
				},
				Action: "push",
			},
		})
}

func TestInvalidScope(t *testing.T) {
	checkResolveScopeSpecifiers(t, []string{"repository"}, []auth.Access{})
	checkResolveScopeSpecifiers(t, []string{"repository:namespace/name"}, []auth.Access{})
	checkResolveScopeSpecifiers(t, []string{":namespace/name:pull"}, []auth.Access{})
	checkResolveScopeSpecifiers(t, []string{"repository:namespace/name:pull:push:get:put:read:write"}, []auth.Access{})
}
