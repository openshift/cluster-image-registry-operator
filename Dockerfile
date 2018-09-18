FROM openshift/origin-release:golang-1.10
COPY . /go/src/github.com/openshift/cluster-image-registry-operator
RUN cd /go/src/github.com/openshift/cluster-image-registry-operator && \
    go build ./cmd/cluster-image-registry-operator

FROM centos:7

RUN useradd cluster-image-registry-operator
USER cluster-image-registry-operator

COPY --from=0 /go/src/github.com/openshift/cluster-image-registry-operator /usr/bin
COPY deploy/image-references deploy/00-crd.yaml deploy/01-namespace.yaml deploy/03-operator.yaml deploy/02-rbac.yaml /manifests/
LABEL io.openshift.release.operator true
