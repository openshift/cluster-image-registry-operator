FROM alpine:3.6

RUN adduser -D cluster-image-registry-operator
USER cluster-image-registry-operator

ADD tmp/_output/bin/cluster-image-registry-operator /usr/local/bin/cluster-image-registry-operator
