FROM registry.svc.ci.openshift.org/openshift/release:golang-1.12 AS builder
WORKDIR /go/src/github.com/openshift/cluster-image-registry-operator
COPY . .
RUN make build

FROM registry.svc.ci.openshift.org/openshift/origin-v4.0:base
COPY images/bin/entrypoint.sh /usr/bin/
COPY manifests/image-references manifests/0* /manifests/
COPY vendor/github.com/openshift/api/imageregistry/v1/00-crd.yaml /manifests/
COPY --from=builder /go/src/github.com/openshift/cluster-image-registry-operator/tmp/_output/bin/cluster-image-registry-operator /usr/bin/
RUN ln /usr/bin/cluster-image-registry-operator /usr/bin/cluster-image-registry-operator-watch && \
    chmod -R g+w /etc/pki/ca-trust/extracted/pem/

ENTRYPOINT ["/usr/bin/entrypoint.sh"]

LABEL io.openshift.release.operator true
