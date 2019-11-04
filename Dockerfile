FROM registry.svc.ci.openshift.org/openshift/release:golang-1.12 AS builder
WORKDIR /go/src/github.com/openshift/cluster-image-registry-operator
COPY . .
RUN make build

FROM registry.svc.ci.openshift.org/openshift/origin-v4.0:base
COPY --from=builder /go/src/github.com/openshift/cluster-image-registry-operator/tmp/_output/bin/cluster-image-registry-operator /usr/bin/
RUN ln /usr/bin/cluster-image-registry-operator /usr/bin/cluster-image-registry-operator-watch
RUN useradd cluster-image-registry-operator
USER cluster-image-registry-operator
COPY manifests/image-references manifests/0* /manifests/
LABEL io.openshift.release.operator true
