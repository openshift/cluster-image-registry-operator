package auth

import (
	"context"
	"regexp"
	"strings"

	dcontext "github.com/docker/distribution/context"
	"github.com/docker/distribution/registry/auth"
)

// ResolveScopeSpecifiers converts a list of scope specifiers from a token
// request's `scope` query parameters into a list of standard access objects.
func ResolveScopeSpecifiers(ctx context.Context, scopeSpecs []string) []auth.Access {
	requestedAccessSet := make(map[auth.Access]struct{}, 2*len(scopeSpecs))
	requestedAccessList := make([]auth.Access, 0, len(requestedAccessSet))

	for _, scopeSpecifier := range scopeSpecs {
		// There should be 3 parts, separated by a `:` character.
		parts := strings.SplitN(scopeSpecifier, ":", 4)

		if len(parts) != 3 {
			dcontext.GetLogger(ctx).Infof("ignoring unsupported scope format %s", scopeSpecifier)
			continue
		}

		resourceType, resourceClass := splitResourceClass(parts[0])
		if resourceType == "" {
			continue
		}

		resourceName, actions := parts[1], parts[2]

		// Actions should be a comma-separated list of actions.
		for _, action := range strings.Split(actions, ",") {
			requestedAccess := auth.Access{
				Resource: auth.Resource{
					Type:  resourceType,
					Class: resourceClass,
					Name:  resourceName,
				},
				Action: action,
			}
			if _, ok := requestedAccessSet[requestedAccess]; !ok {
				requestedAccessList = append(requestedAccessList, requestedAccess)
				requestedAccessSet[requestedAccess] = struct{}{}
			}
		}
	}

	return requestedAccessList
}

var typeRegexp = regexp.MustCompile(`^([a-z0-9]+)(?:\(([a-z0-9]+)\))?$`)

func splitResourceClass(t string) (string, string) {
	matches := typeRegexp.FindStringSubmatch(t)
	if matches != nil {
		return matches[1], matches[2]
	}
	return "", ""
}
