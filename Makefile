IMAGE ?= docker.io/openshift/origin-cluster-image-registry-operator
TAG ?= latest
PROG  := cluster-image-registry-operator

all: generate build build-image verify
.PHONY: all

generate:
	./hack/codegen/update-generated.sh
	./hack/codegen/update-crd.sh
.PHONY: generate

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
	./hack/test-go.sh -count 1 -timeout 30m -v$${WHAT:+ -run="$$WHAT"} ./test/e2e/
.PHONY: test-e2e

verify: verify-crd verify-fmt
.PHONY: verify

verify-crd:
	./hack/codegen/update-crd.sh --verify-only
.PHONY: verify-crd

verify-fmt:
	./hack/verify-gofmt.sh
.PHONY: verify-gofmt

clean:
	rm -rf tmp
.PHONY: clean