FROM openshift/origin-release:golang-1.10 as builder
WORKDIR /go/src/github.com/openshift/cluster-image-registry-operator
COPY . .
RUN make build

FROM centos:7
RUN useradd cluster-image-registry-operator
USER cluster-image-registry-operator
COPY --from=builder /go/src/github.com/openshift/cluster-image-registry-operator/tmp/_output/bin/cluster-image-registry-operator /usr/bin
# these manifests are necessary for the installer
COPY deploy/image-references deploy/*.yaml /manifests/
LABEL io.openshift.release.operator true
