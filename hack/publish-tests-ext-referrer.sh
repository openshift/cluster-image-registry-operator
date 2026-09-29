#!/usr/bin/env bash

set -euo pipefail

if [[ $# -ne 1 ]]; then
	echo "usage: $0 <registry/repository:tag>" >&2
	exit 2
fi

image_ref="$1"
if [[ "$image_ref" != *:* || "$image_ref" == *@* ]]; then
	echo "image reference must include a tag" >&2
	exit 2
fi

for command in docker oras jq cmp mktemp gzip; do
	if ! command -v "$command" >/dev/null 2>&1; then
		echo "required command not found: $command" >&2
		exit 1
	fi
done

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
binary_name="cluster-image-registry-operator-tests-ext.gz"
artifact_type="application/vnd.openshift.tests-extension.v1+gzip"
workdir="$(mktemp -d)"
builder_tag="localhost/cluster-image-registry-operator-tests-ext-builder:$(basename "$workdir")"
builder_container=""
registry_host="${image_ref%%/*}"
namespace="${image_ref#*/}"
namespace="${namespace%%/*}"
scoped_auth_key="$registry_host/$namespace"
docker_config="${DOCKER_CONFIG:-$HOME/.docker}/config.json"
oras_args=()

cleanup() {
	if [[ -n "$builder_container" ]]; then
		docker rm "$builder_container" >/dev/null 2>&1 || true
	fi
	docker rmi "$builder_tag" >/dev/null 2>&1 || true
	rm -rf "$workdir"
}
trap cleanup EXIT

docker build --target builder -t "$builder_tag" "$repo_root"
builder_container="$(docker create "$builder_tag")"
docker cp "$builder_container:/go/src/github.com/openshift/cluster-image-registry-operator/tmp/_output/bin/$binary_name" "$workdir/$binary_name"
gzip -t "$workdir/$binary_name"

docker build -t "$image_ref" "$repo_root"
docker run --rm --entrypoint /bin/sh "$image_ref" -c "test ! -e /usr/bin/$binary_name"

# Docker auth configs can contain both host-wide and namespace-specific logins.
# Podman and ORAS need a host-level entry to use the namespace-specific login.
if [[ -f "$docker_config" ]] && jq -e --arg key "$scoped_auth_key" '.auths[$key] != null' "$docker_config" >/dev/null; then
	authfile="$workdir/auth.json"
	jq -e --arg host "$registry_host" --arg key "$scoped_auth_key" '{auths: {($host): .auths[$key]}}' "$docker_config" > "$authfile"
	chmod 600 "$authfile"
	oras_args=(--registry-config "$authfile")
	REGISTRY_AUTH_FILE="$authfile" docker push "$image_ref"
else
	docker push "$image_ref"
fi

image_digest_ref="$(oras resolve "${oras_args[@]}" --full-reference "$image_ref")"
referrer_digest="$(
	cd "$workdir"
	oras attach \
		"${oras_args[@]}" \
		--distribution-spec v1.1-referrers-api \
		--artifact-type "$artifact_type" \
		--format json \
		"$image_digest_ref" \
		"$binary_name:application/gzip" | jq -er '.digest'
)"

oras discover \
	"${oras_args[@]}" \
	--distribution-spec v1.1-referrers-api \
	--artifact-type "$artifact_type" \
	--format json \
	"$image_digest_ref" | jq -e --arg digest "$referrer_digest" '(.manifests // .referrers) | any(.digest == $digest)' >/dev/null

mkdir "$workdir/download"
oras pull "${oras_args[@]}" -o "$workdir/download" "${image_digest_ref%@*}@$referrer_digest"
cmp "$workdir/$binary_name" "$workdir/download/$binary_name"

printf 'Image: %s\nImage digest: %s\nReferrer digest: %s\n' "$image_ref" "$image_digest_ref" "$referrer_digest"
