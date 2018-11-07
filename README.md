# cluster-image-registry-operator


The image registry operator installs+maintains the integrated image registry on a cluster.

# Manual deployment

In order to deploy the `cluster-image-registry-operator` from scratch, one needs to:

1. Build operator image:
```sh
operator-sdk build docker.io/openshift/cluster-image-registry-operator:latest
```

2. As an admin create `openshift-image-registry` namespace:
```sh
oc apply -f deploy/namespace.yaml
```

3. Create `cluster-image-registry-operator` cluster role. This is necessary because
the operator creates a cluster role for the `image-registry`:
```sh
oc apply -n openshift-image-registry -f deploy/rbac.yaml
```

4. Create CRD definition:
```sh
oc apply -n openshift-image-registry -f deploy/crd.yaml
```

5. Create a custom resource for the `cluster-image-registry-operator`. This must be done
before the operator is started, otherwise the operator will generate the resource itself
and some fields can not be changed after creation. As an example, you can use `deploy/cr.yaml`.
```sh
oc apply -n openshift-image-registry -f deploy/cr.yaml
```

6. Deploy operator:
```sh
cat deploy/operator.yaml |
    sed 's/imagePullPolicy: Always/imagePullPolicy: Never/' |
    oc apply -n openshift-image-registry -f -
```

If you want a simple installation for tests, then you can simply run the [hack/deploy.sh](https://github.com/openshift/cluster-image-registry-operator/blob/master/hack/deploy.sh) script.

# Custom resource documentation

TODO

# CI & tests

TODO
