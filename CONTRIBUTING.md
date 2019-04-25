# Contributing to the Openshift Image Registry Operator

The registry operator manages a singleton instance of the openshift registry.  It manages all configuration of the registry including creating storage.

## Getting the code

To get started, [fork](https://help.github.com/articles/fork-a-repo) the [openshift/cluster-image-registry-operator](https://github.com/openshift/cluster-image-registry-operator) repo.

## Developing

### Testing on an OpenShift Cluster

The easiest way to test your changes is to launch an OpenShift 4.x cluster.
First, go to [try.openshift.com](https://try.openshift.com) to obtain a pull secret and download the installer.
Follow the instructions to launch a cluster on AWS.

If you want the latest `openshift-install` and `oc` clients, go to the [openshift-release](https://openshift-release.svc.ci.openshift.org/) 
page, select the channel and build you wish to install, and download the respective `oc` and `openshift-installer` binaries.
There are three types of channels you can obtain the installer from:

1. `4-stable` - these are stable releases of OpenShift 4, corresponding to GA or beta releases.
2. `4.x.0-nightly` - nightly development releases, with payloads published to quay.io.
3. `4.x.0-ci` - bleeding-edge releases published to the OpenShift CI imagestreams.

**Note**: Installs from the `4.x.0-ci` channel require a pull secret to `registry.svc.ci.openshift.org`, which is only available to Red Hat OpenShift developers.

After your cluster is installed, you will need to do the following:

1. Patch the cluster version so that you can launch your own image-registry operator image:

```
$ oc patch clusterversion/version --patch '{"spec":{"overrides":[{"kind":"Deployment", "name":"cluster-image-registry-operator","namespace":"openshift-image-registry","unmanaged":true}]}}' --type=merge
```

2. Make your code changes and build the binary with `make build`.
3. Build the image using the `Dockerfile` file, giving it a unique tag:

```
$ make build-image IMAGE=<MYREPO>/<MYIMAGE> TAG=<MYTAG> 
```

or if you are using `buildah`:

```
$ buildah bud -t <MYREPO>/<MYIMAGE>:<MYTAG> -f Dockerfile .
```

4. Push the image to a registry accessible from the cluster (e.g. your repository on quay.io).
5. Patch the Deployment in the override above to instruct the cluster to use your builder image:

```
$ oc patch deployment cluster-image-registry-operator -n openshift-image-registry --patch '{"spec":{"template":{"spec":{"containers":[{"name":"cluster-image-registry-operator","image":"<MYREPO>/<MYIMAGE>:<MYTAG>"}]}}}}' --type=strategic
```

6. Watch the openshift cluster image-registry operator replicaset rollout (this can take a few minutes):

```
$ oc get rs cluster-image-registry-operator-<hash> -n openshift-image-registry -w
```
## Submitting a Pull Request

Once you are satisfied with your code changes, you may submit a pull request to the [openshift/cluster-image-registry-operator](https://github.com/openshift/cluster-image-registry-operator) repo.
A member of the OpenShift Developer Experience team will evaluate your change for approval.