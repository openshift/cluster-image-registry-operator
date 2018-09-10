all: generate build build-image
generate:
	operator-sdk generate k8s
build:
	go build ./cmd/cluster-image-registry-operator
build-image:
	docker build -t docker.io/openshift/origin-cluster-image-registry-operator:latest .
test:
	go test ./...
clean:
	rm cluster-image-registry-operator