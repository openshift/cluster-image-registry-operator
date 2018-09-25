IMAGE ?= docker.io/openshift/origin-cluster-image-registry-operator:latest
PROG  := cluster-image-registry-operator

.PHONY: all generate build build-image build-devel-image test test-unit test-e2e clean

all: generate build build-image

generate:
	operator-sdk generate k8s

build:
	./tmp/build/build.sh

build-image:
	docker build -t "$(IMAGE)" .

build-devel-image:
	operator-sdk build "$(IMAGE)"

test: test-unit test-e2e

test-unit:
	go test ./cmd/... ./pkg/...

test-e2e:
	mkdir -p -m775 ./deploy/test/
	cat ./deploy/03-sa.yaml ./deploy/04-operator.yaml >./deploy/test/namespace-manifests.yaml
	operator-sdk test local ./test/e2e --global-manifest=./deploy/00-crd.yaml --namespaced-manifest=./deploy/test/namespace-manifests.yaml

clean:
	rm -- "$(PROG)"
