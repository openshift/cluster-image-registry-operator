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

verify: verify-fmt
.PHONY: verify

verify-fmt:
	./hack/verify-gofmt.sh
.PHONY: verify-gofmt

update-deps:
	go get -d -u \
		github.com/openshift/api@release-4.4 \
		github.com/openshift/client-go@release-4.4 \
		github.com/openshift/library-go@release-4.4
	go get -u=patch ./cmd/... ./pkg/... ./test/... sigs.k8s.io/structured-merge-diff@v1.0.1-0.20191108220359-b1b620dd3f06
	go mod tidy
	go mod vendor
.PHONY: update-deps

clean:
	rm -rf tmp
.PHONY: clean
