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
      - group: apps/v1
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
