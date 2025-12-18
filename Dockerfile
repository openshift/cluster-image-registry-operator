FROM registry.ci.openshift.org/ocp/builder:rhel-9-golang-1.24-openshift-4.22 AS builder
WORKDIR /go/src/github.com/openshift/cluster-image-registry-operator
COPY . .
RUN make build \
    && gzip tmp/_output/bin/cluster-image-registry-operator-tests-ext 
WORKDIR /go/src/github.com/openshift/cluster-image-registry-operator/cmd/move-blobs
RUN go build -o ../../tmp/_output/bin/move-blobs

FROM registry.ci.openshift.org/ocp/4.22:base-rhel9
COPY images/bin/entrypoint.sh /usr/bin/
COPY manifests/image-references manifests/0* /manifests/
COPY vendor/github.com/openshift/api/imageregistry/v1/**/*.crd.yaml /manifests/
COPY --from=builder /go/src/github.com/openshift/cluster-image-registry-operator/tmp/_output/bin/cluster-image-registry-operator /usr/bin/
COPY --from=builder /go/src/github.com/openshift/cluster-image-registry-operator/tmp/_output/bin/cluster-image-registry-operator-tests-ext.gz /usr/bin/
COPY --from=builder /go/src/github.com/openshift/cluster-image-registry-operator/tmp/_output/bin/move-blobs /usr/bin/
RUN chmod -R g+w /etc/pki/ca-trust/extracted/pem/

ENTRYPOINT ["/usr/bin/entrypoint.sh"]

LABEL io.openshift.release.operator true
