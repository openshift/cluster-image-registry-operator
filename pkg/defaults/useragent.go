package defaults

import "github.com/openshift/cluster-image-registry-operator/pkg/version"

// UserAgent identifies the operator in HTTP requests.
// Azure SDK ApplicationID has a 24 character limit, so keep this short.
var UserAgent = "ocp-ciro/" + version.Version
