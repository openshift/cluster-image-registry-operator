FROM registry.ci.openshift.org/ocp/builder:rhel-8-golang-1.16-openshift-4.10 AS builder
WORKDIR /go/src/github.com/openshift/cluster-image-registry-operator
COPY . .
RUN make build

FROM registry.ci.openshift.org/ocp/4.10:base
COPY images/bin/entrypoint.sh /usr/bin/
COPY manifests/image-references manifests/0* /manifests/
COPY vendor/github.com/openshift/api/imageregistry/v1/*-crd.yaml /manifests/
COPY --from=builder /go/src/github.com/openshift/cluster-image-registry-operator/tmp/_output/bin/cluster-image-registry-operator /usr/bin/
RUN chmod -R g+w /etc/pki/ca-trust/extracted/pem/

ENTRYPOINT ["/usr/bin/entrypoint.sh"]

LABEL io.openshift.release.operator true
