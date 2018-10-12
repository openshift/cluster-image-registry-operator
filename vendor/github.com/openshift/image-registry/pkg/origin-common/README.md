## Packages based on the code from the OpenShift Origin repository

### clientcmd

The clientcmd package is a reduced copy of [github.com/openshift/origin/pkg/cmd/util/clientcmd](https://godoc.org/github.com/openshift/origin/pkg/cmd/util/clientcmd).

The code is almost untouched, but there are some differences:

  * some dependencies were merged into this package (getEnv, Addr, recommendedHomeFile, etc.),
  * it doesn't support migrations for `KUBECONFIG` (i.e. the old default is ignored, which is `.kube/.config`),
  * it uses the field `openshift.kubeconfig` from our config instead of the `--config` flag.

### image/apis/image

This is a significantly reduced set of code related to the internal api objects defined in [github.com/openshift/origin/pkg/image
image](https://godoc.org/github.com/openshift/origin/pkg/image
image).  

It includes the docker type definitions, constants, and helpers.  The only changes are either deletions or package import updates to consume
the apimachinery/api/client-go repositories instead of origin/kubernetes.


### image/registryclient

This code is copied from [github.com/openshift/origin/pkg/image/registryclient](https://godoc.org/github.com/openshift/origin/pkg/image/registryclient).

The only change is to use the docker distribution digest package instead of of opencontainers go-digest, due to the level of docker distribution
used by the image-registry repository and a few other import reconcilations.

### quota/util

This code is copied from [github.com/openshift/origin/pkg/quota/util](https://godoc.org/github.com/openshift/origin/pkg/quota/util).

The test code had some schema related bootstrapping logic added to break the dependency on origin.

### util

This package consists of helper code is copied from various locations in origin.  Most of it came from [https://github.com/openshift/origin/blob/4bb21512b79d80e6dd36044ac0f47ab84aa731da/pkg/image/apis/image/v1/helpers.go#L1](helper logic) in origin.

### util/httprequest

This code is copied untouched from [github.com/openshift/origin/pkg/util/httprequest](https://godoc.org/github.com/openshift/origin/pkg/util/httprequest).
