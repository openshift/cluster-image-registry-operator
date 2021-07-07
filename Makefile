IMAGE ?= docker.io/openshift/origin-cluster-image-registry-operator
TAG ?= latest
PROG  := cluster-image-registry-operator

GOLANGCI_LINT = _output/tools/golangci-lint
GOLANGCI_LINT_CACHE = $(PWD)/_output/golangci-lint-cache
GOLANGCI_LINT_VERSION = v1.24

GO_REQUIRED_MIN_VERSION = 1.16

include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
    targets/openshift/operator/profile-manifests.mk \
)

$(call add-profile-manifests,manifests,./profile-patches,./manifests)

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

test-unit:
	./hack/test-go.sh ./cmd/... ./pkg/... ./test/framework/...
.PHONY: test-unit

test-e2e:
	./hack/test-go.sh -count 1 -timeout 2h -v$${WHAT:+ -run="$$WHAT"} ./test/e2e/
.PHONY: test-e2e

.PHONY: verify

verify-fmt:
	./hack/verify-gofmt.sh
verify: verify-fmt
.PHONY: verify-fmt

$(GOLANGCI_LINT):
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(dir $@) v1.24.0

verify-golangci-lint: $(GOLANGCI_LINT)
	GOLANGCI_LINT_CACHE=$(GOLANGCI_LINT_CACHE) $(GOLANGCI_LINT) run --timeout=300s ./cmd/... ./pkg/... ./test/...

verify: verify-golangci-lint
.PHONY: verify-golangci-lint

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
