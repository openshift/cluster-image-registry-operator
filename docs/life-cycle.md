# The image registry life cycle

## Installation of the Operator

The Operator is a part of the OpenShift payload. OpenShift operators provide their manifests in their container image in the `/manifests/` directory.

Most of the Image Registry Operator manifests you can find in [/manifests/](../manifests/). One exception is custom resource definitions that live in [github.com/openshift/api/imageregistry/v1](https://github.com/openshift/api/tree/master/imageregistry/v1). They are part of the container image, so they are vendored into this repository.

The `cluster-version-operator` creates manifests from the payload images on freshly installed clusters. Read [the CVO documentation](https://github.com/openshift/enhancements/tree/master/dev-guide/cluster-version-operator) for details.

## Creation of a config object for the Operator

Once the Image Registry Operator is deployed on the cluster, it creates a default config object: `configs.imageregistry.operator.openshift.io/cluster`.

On S3 it looks like:

```yaml
apiVersion: imageregistry.operator.openshift.io/v1
kind: Config
metadata:
  finalizers:
  - imageregistry.operator.openshift.io/finalizer
  name: cluster
spec:
  logLevel: Normal
  managementState: Managed
  operatorLogLevel: Normal
  replicas: 2
  rolloutStrategy: RollingUpdate
  storage:
    s3: {}
```

The Operator uses this object to report about its state.

## Reconciliation loop

### Creation of storage

Once the Operator sees that the storage configuration is incomplete, the Operator tries to complete it. If `spec.storage` is empty, the Operator detects the best storage for the current platform using [storage.GetPlatformStorage](https://pkg.go.dev/github.com/openshift/cluster-image-registry-operator/pkg/storage#GetPlatformStorage).

Then the Operator creates an instance of [a storage driver](https://pkg.go.dev/github.com/openshift/cluster-image-registry-operator/pkg/storage#Driver). Storage drivers allow the Operator to interact with different storage providers. The storage driver has the method `CreateStorage` that knows how to complete its configuration and create necessary objects. This method should be idempotent, i.e. if this method is run twice, the second run should just check that everything is properly configured.

As the Operator may want to reconcile storage fairly often and the method `CreateStorage` may be expensive, the driver method `StorageChanged` allows the Operator to avoid using it most of the time.

See [(*resource.Generator).syncStorage](../pkg/resource/generator.go) for implementation details.

### A note about storage configuration

The storage driver should put dynamically generated values into the config object if they are not intended to be changed.

For example, when the Operator creates an S3 bucket, its name will stay the same even if the Operator rules for the name are changed in future releases. So the S3 bucket name should be put into the config object.

When the Operator should use a cluster scoped parameter that can be changed, the parameter in the config object should stay empty and should be used only by customers if they want to override the value.

### A note about storage secrets

The config object providers ability to override any parameter for storage, but it cannot contain secret values. If the customer wants to override a secret, they should use the secret `image-registry-private-configuration-user`.

### Creation of the image registry objects

The image registry deployment and related objects are created by the Operator's main controller. See [resource.Generator](../pkg/resource/generator.go) for details.

### The image pruner

The image pruner is maintained by [ImagePrunerController](https://pkg.go.dev/github.com/openshift/cluster-image-registry-operator/pkg/operator#ImagePrunerController). It has its own config object: `imagepruner.imageregistry.operator.openshift.io/cluster`.

### The cluster operator object

The cluster operator object `image-registry` is maintained by [ClusterOperatorStatusController](https://pkg.go.dev/github.com/openshift/cluster-image-registry-operator/pkg/operator#ClusterOperatorStatusController). It aggregates conditions from the Operator config objects.
