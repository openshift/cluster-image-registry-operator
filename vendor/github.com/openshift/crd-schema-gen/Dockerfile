FROM registry.svc.ci.openshift.org/openshift/release:golang-1.12 AS builder
WORKDIR /go/src/github.com/openshift/crd-schema-gen
COPY . .
ENV GO_PACKAGE github.com/openshift/crd-schema-gen
RUN go build -o crd-schema-gen cmd/crd-schema-gen/main.go

FROM registry.svc.ci.openshift.org/openshift/release:golang-1.12
COPY --from=builder /go/src/github.com/openshift/crd-schema-gen/crd-schema-gen /usr/local/bin
ENV GOPATH=/go
ENTRYPOINT ["/usr/local/bin/crd-schema-gen"]
