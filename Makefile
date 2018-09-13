IMAGE ?= docker.io/openshift/origin-cluster-image-registry-operator:latest
PROG  := cluster-image-registry-operator

.PHONY: all generate build clean test build-image build-devel-image

all: generate build build-image

generate:
	operator-sdk generate k8s

build:
	go build "./cmd/$(PROG)"

build-image:
	docker build -t "$(IMAGE)" .

build-devel-image: build
	docker build -t "$(IMAGE)" -f Dockerfile-dev .

test:
	go test ./...

clean:
	rm -- "$(PROG)"
