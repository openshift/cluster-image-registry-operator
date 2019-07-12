# crd-schema-gen
Containerized CRD validation generation

Based on a [fork](https://github.com/openshift/kubernetes-sigs-controller-tools) of functionality found in https://github.com/kubernetes-sigs/controller-tools/tree/master/cmd/crd

Looks at types found in currently available CRDs under a project's `manifests/` directory and traverses `openshift/api` types to find matching Golang structs, then generates OpenAPI validations for those CRDs

## Running

Build this image:
```
make images
```

Then from your operator's repository run:

```
PKG=${PWD#${GOPATH}/}
docker run -v ${PWD}:/go/${PKG}:Z -w /go/${PKG} openshift/origin-crd-schema-gen --apis-dir vendor/github.com/openshift/api/operator/v1
```

In this case we mount our local `manifests` directory where our CRDs reside and the local directory of our API definitions. Then pass the name of that location (relative to the crd-schema-gen GOPATH) to the container (`openshift/api`).