IMAGE ?= docker.io/openshift/origin-cluster-image-registry-operator
TAG ?= latest
PROG  := cluster-image-registry-operator

all: build build-image verify
.PHONY: all

build:
	./hack/build/build.sh
.PHONY: build

build-image:
	docker build -t "$(IMAGE):$(TAG)" .
.PHONY: build-image

test: test-unit test-e2e
.PHONY: test

test-unit: verify
	./hack/test-go.sh ./cmd/... ./pkg/...
.PHONY: test-unit

test-e2e:
	./hack/test-go.sh -count 1 -timeout 2h -v$${WHAT:+ -run="$$WHAT"} ./test/e2e/
.PHONY: test-e2e

verify: verify-fmt verify-sec
.PHONY: verify

verify-fmt:
	./hack/verify-gofmt.sh
.PHONY: verify-gofmt

verify-sec:
	go get -u github.com/securego/gosec/cmd/gosec
	gosec -severity medium -confidence medium -exclude G304 -quiet ./...
.PHONY: verify-sec

update-deps:
	go get -d -u \
		k8s.io/apiextensions-apiserver@kubernetes-1.16.2 \
		k8s.io/api@kubernetes-1.16.2 \
		k8s.io/apimachinery@kubernetes-1.16.2 \
		k8s.io/apiserver@kubernetes-1.16.2 \
		k8s.io/client-go@kubernetes-1.16.2 \
		k8s.io/code-generator@kubernetes-1.16.2 \
		sigs.k8s.io/structured-merge-diff@v0.0.0-20190817042607-6149e4549fca \
		github.com/prometheus/client_golang@v0.9.2 \
		github.com/openshift/api@release-4.3 \
		github.com/openshift/client-go@release-4.3 \
		github.com/openshift/library-go@release-4.3
	go get -u=patch ./cmd/... ./pkg/... ./test/...
	go mod tidy
	go mod vendor
.PHONY: update-deps

clean:
	rm -rf tmp
.PHONY: clean
