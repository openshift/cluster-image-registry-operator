IMAGE ?= docker.io/openshift/origin-cluster-image-registry-operator
TAG ?= latest
PROG  := cluster-image-registry-operator

GOLANGCI_LINT = _output/tools/golangci-lint
GOLANGCI_LINT_CACHE = $(PWD)/_output/golangci-lint-cache
GOLANGCI_LINT_VERSION = v1.51.0

GO_REQUIRED_MIN_VERSION = 1.16

include $(addprefix ./vendor/github.com/openshift/build-machinery-go/make/, \
    targets/help.mk \
    targets/golang/verify-update.mk \
    targets/openshift/deps.mk \
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
	./hack/test-go.sh -count 1 -timeout 110m -v$${WHAT:+ -run="$$WHAT"} ./test/e2e/
.PHONY: test-e2e

verify: verify-gofmt verify-deps
.PHONY: verify

$(GOLANGCI_LINT):
	if [ ! -e $@ ] || ! $@ --version | grep -q $(patsubst v%,%,$(GOLANGCI_LINT_VERSION)); then \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(dir $@) $(GOLANGCI_LINT_VERSION); \
	fi
.PHONY: $(GOLANGCI_LINT)

verify-golangci-lint: $(GOLANGCI_LINT)
	GOLANGCI_LINT_CACHE=$(GOLANGCI_LINT_CACHE) $(GOLANGCI_LINT) run --timeout=300s ./cmd/... ./pkg/... ./test/...
verify: verify-golangci-lint
.PHONY: verify-golangci-lint

update: update-gofmt
.PHONY: update

clean:
	rm -rf tmp
.PHONY: clean
