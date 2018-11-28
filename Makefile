IMAGE ?= docker.io/openshift/origin-cluster-image-registry-operator
TAG ?= latest
PROG  := cluster-image-registry-operator

.PHONY: all generate build build-image build-devel-image test test-unit test-e2e verify clean

all: generate build build-image verify

generate:
	./hack/codegen/update-generated.sh

build:
	./hack/build/build.sh

build-image:
	docker build -t "$(IMAGE):$(TAG)" .

test: test-unit test-e2e

test-unit:
	go test ./cmd/... ./pkg/...

test-e2e:
	GOCACHE=off go test -v$${WHAT:+ -run="$$WHAT"} ./test/e2e/

verify:
	hack/verify.sh

clean:
	rm -rf tmp
