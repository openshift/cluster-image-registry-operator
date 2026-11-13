# How to deploy a development build of the Image Registry Operator

## Prerequisites

 * An OpenShift cluster.
 * A public image repository (for example, you can create a public repository on [quay.io](https://quay.io/)).
 * (optional) Credentials from [the app.ci cluster](https://console-openshift-console.apps.ci.l2s4.p1.openshiftapps.com/).

## Logging into the app.ci cluster and its registry

1. Copy the login command from <https://console-openshift-console.apps.ci.l2s4.p1.openshiftapps.com/> and run it.
2. Rename the context for the `app.ci` cluster:

    ```
    oc config rename-context "$(oc config current-context)" app.ci
    ```

3. Login into the registry `registry.ci.openshift.org`:

    ```
    oc --context=app.ci whoami -t | docker login -u unused --password-stdin "$(oc --context=app.ci registry info --public=true)"
    ```

## Disabling cluster-version-operator for the Operator objects

The repository contains a script that disables the objects management: [hack/add-cvo-overrides.sh](../hack/add-cvo-overrides.sh).

You can also do it manually:

1. Open an editor for clusterversion.config.openshift.io/version:

    ```
    oc edit clusterversion.config.openshift.io/version
    ```

2. Add your entries to the overrides list or create it if it does not exist:

    ```yaml
    spec:
      overrides:
      - group: apps
        kind: Deployment
        name: cluster-image-registry-operator
        namespace: openshift-image-registry
        unmanaged: true
    ```

If you want to edit other objects that are managed by CVO (for example, CustomResourceDefinitions), don't forget to add entries for them.

## Building and deploying a new container image

1. Go to the directory with the Operator sources:

    ```
    cd ./openshift/cluster-image-registry-operator
    ```

2. Build a new image:

    ```
    make build-image IMAGE=quay.io/rh-obulatov/cluster-image-registry-operator
    ```

    If you don't have credentials for the `app.ci` cluster, you can build an OKD image:

    ```
    docker build -t quay.io/rh-obulatov/cluster-image-registry-operator -f Dockerfile.okd .
    ```

3. Push the new image:

    ```
    docker push quay.io/rh-obulatov/cluster-image-registry-operator
    ```

4. Deploy the new build:

    ```
    oc -n openshift-image-registry set image deploy/cluster-image-registry-operator cluster-image-registry-operator="$(docker inspect --format='{{index .RepoDigests 0}}' quay.io/rh-obulatov/cluster-image-registry-operator)"
    ```

5. Wait until the new image is deployed:

    ```
    oc -n openshift-image-registry get pods -l name=cluster-image-registry-operator -o custom-columns="NAME:.metadata.name,STATUS:.status.phase,IMAGE:.spec.containers[0].image"
    ```

6. Your operator is deployed.

## Publishing the tests extension as an OCI referrer (POC)

The runtime image built from `Dockerfile` does not contain
`cluster-image-registry-operator-tests-ext.gz`. The compressed test binary is
published as a separate OCI artifact whose subject is the pushed image digest.
This POC builds one image for the local architecture.

Log in to the builder registry and Quay, and install `docker`, `oras`, and `jq`.
Then run the publish script with a unique tag:

```bash
image="quay.io/sdodsonrht/cluster-image-registry-operator:referrers-poc-$(git rev-parse --short=12 HEAD)-$(date -u +%Y%m%d%H%M%S)"
./hack/publish-tests-ext-referrer.sh "$image"
```

The script extracts the gzip from the Dockerfile's builder stage, builds and
pushes the runtime image, and attaches the gzip using the OCI 1.1 referrers
API. It checks that the file is absent from the runtime image and that the
downloaded referrer matches the built file. It prints the image and referrer
digests. To discover and retrieve the artifact later, use those digests:

```bash
oras discover --distribution-spec v1.1-referrers-api \
  --artifact-type application/vnd.openshift.tests-extension.v1+gzip \
  quay.io/sdodsonrht/cluster-image-registry-operator@<image-digest>
oras pull -o ./tests-extension \
  quay.io/sdodsonrht/cluster-image-registry-operator@<referrer-digest>
gzip -dc ./tests-extension/cluster-image-registry-operator-tests-ext.gz > ./tests-extension/cluster-image-registry-operator-tests-ext
chmod +x ./tests-extension/cluster-image-registry-operator-tests-ext
./tests-extension/cluster-image-registry-operator-tests-ext list suites
```

If the Quay repository is private, use a login with pull access for these
commands. The publish script uses a namespace-specific login from
`~/.docker/config.json` when one is available.

The corresponding `openshift-tests` consumer change discovers this artifact
before trying the legacy path inside the operator image. Release promotion
and mirroring must copy the referrer alongside the image for this to work in
release payloads.
