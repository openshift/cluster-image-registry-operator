# Architecture: cluster-image-registry-operator

## Overview

The cluster-image-registry-operator manages the singleton OpenShift internal container image registry. It is responsible for deploying the registry, provisioning cloud storage, managing TLS certificates and routes, handling image pruning, and reporting operator health.

The operator runs in the `openshift-image-registry` namespace and reconciles the `configs.imageregistry.operator.openshift.io/cluster` custom resource.

## Controllers

The operator runs 11+ concurrent controllers, each with its own watches and responsibilities:

| Controller | Purpose |
|-----------|---------|
| **Controller** (main) | Reconciles the Config CR — manages registry Deployment, Service, Route, RBAC, storage, and secrets |
| **ClusterOperatorStatusController** | Aggregates conditions into the `ClusterOperator` CR for cluster-level health reporting |
| **ImageConfigController** | Syncs external image configuration from `config.openshift.io/images` |
| **ImageRegistryCertificatesController** | Manages TLS certificates for the registry |
| **NodeCADaemonController** | Ensures node CA certificates are synced to all nodes via a DaemonSet |
| **ImagePrunerController** | Manages the image pruner CronJob workload |
| **AWSTagController** | Applies required tags to AWS S3 buckets |
| **AzureStackCloudController** | Handles Azure Stack-specific cloud configuration |
| **AzurePathFixController** | Corrects Azure blob storage path ACLs |
| **MetricsController** | Exposes Prometheus metrics on port 60000 |
| **LoggingController** | Manages dynamic log level configuration |

All controllers are started in `pkg/operator/starter.go` via `RunOperator()`, which sets up shared informer factories across multiple namespaces (`openshift-image-registry`, `openshift-config`, `openshift-config-managed`, `kube-system`).

## Reconciliation Flow

The main controller uses a single workqueue key (`"changes"`) — all watched events coalesce into one sync cycle:

```text
Event (Config CR, Deployment, Secret, ConfigMap, etc.)
  → Enqueue "changes"
    → sync()
      ├── Bootstrap: create default Config CR if missing (with platform-detected storage defaults)
      ├── Check management state:
      │   ├── Removed → tear down registry + storage via finalizer
      │   ├── Unmanaged → skip reconciliation
      │   └── Managed → full reconciliation
      ├── Resource generation (resource.Generator.Apply):
      │   ├── Verify config completeness
      │   ├── Create/update Deployment, Service, Route, PDB, RBAC, etc.
      │   └── Configure storage-specific environment and volumes
      └── Status update: conditions, ready replicas, storage state
```

The Config CR is read directly from the API server (not from the informer cache) to avoid staleness during rapid updates.

## Storage Architecture

Multi-cloud storage support is implemented via the `storage.Driver` interface:

```go
type Driver interface {
    CABundle() (string, bool, error)
    ConfigEnv() (envvar.List, error)
    Volumes() ([]Volume, []VolumeMount, error)
    VolumeSecrets() (map[string]string, error)
    CreateStorage(*Config) error
    StorageExists(*Config) (bool, error)
    RemoveStorage(*Config) (bool, error)
    StorageChanged(*Config) bool
    ID() string
}
```

Each cloud provider implements this interface:

| Driver | Backend | Auto-detected On |
|--------|---------|-----------------|
| `s3` | AWS S3 | AWS |
| `azure` | Azure Blob Storage | Azure |
| `gcs` | Google Cloud Storage | GCP |
| `ibmcos` | IBM Cloud Object Storage | IBM Cloud |
| `swift` | OpenStack Swift | OpenStack |
| `pvc` | PersistentVolumeClaim | Baremetal, vSphere |
| `emptydir` | EmptyDir (ephemeral) | Unknown platforms |

Platform detection reads the `config.openshift.io/infrastructures/cluster` resource. Storage configuration is set at bootstrap and is immutable afterward — changing storage type requires deleting and recreating the Config CR.

## Resource Generation

The `pkg/resource/` package generates all Kubernetes resources the registry needs. The `Generator` type orchestrates creation and updates of:

- **Deployment**: Registry pods with storage-specific volumes, environment variables, and update strategy (Recreate for PVC, RollingUpdate for cloud storage)
- **Service**: ClusterIP service for internal access
- **Route**: Optional external route with re-encrypt TLS
- **PodDisruptionBudget**: Availability guarantees
- **RBAC**: ClusterRoles and bindings for registry operations
- **Secrets**: Pull secrets, cloud credentials, TLS certificates
- **CronJob**: Image pruner schedule (managed by ImagePrunerController)
- **DaemonSet**: Node CA certificate sync (managed by NodeCADaemonController)

Each resource type implements a `Getter`/`Mutator` pattern — the generator reads the current state, applies mutations, and diffs against the existing object to determine if an update is needed.

## CRDs

The operator reconciles two custom resources (defined in the `openshift/api` repo):

- **`configs.imageregistry.operator.openshift.io/v1`** — Primary configuration: storage, replicas, routes, logging, management state. Status includes conditions, storage state, and ready replica count.
- **`imagepruners.imageregistry.operator.openshift.io/v1`** — Image pruner configuration: schedule, suspend flag, resource limits, image selectors.

The operator also writes to the **`clusteroperators.config.openshift.io/v1`** resource to report health to the cluster.

## Dependency Map

```text
cluster-image-registry-operator
├── depends on
│   ├── openshift/api              (CRD types: ImageRegistry, ImagePruner, Infrastructure, ClusterOperator)
│   ├── openshift/client-go        (generated clients for OpenShift APIs)
│   ├── openshift/library-go       (controller framework, config observers, status helpers)
│   ├── k8s.io/client-go           (informers, work queues, retry logic)
│   └── Cloud SDKs                 (aws-sdk-go, azure-sdk-for-go, cloud.google.com/go, ibm-cos-sdk-go, gophercloud)
│
├── manages
│   ├── Registry Deployment        (openshift-image-registry/image-registry)
│   ├── Registry Service + Route
│   ├── Cloud storage resources    (S3 buckets, Azure containers, GCS buckets, etc.)
│   ├── Pruner CronJob
│   └── Node CA DaemonSet
│
└── reports to
    └── ClusterOperator CR         (consumed by CVO for cluster health)
```

## Design Decisions

| Decision | Rationale |
|----------|-----------|
| Single workqueue key | All events coalesce to one sync cycle. Simplifies logic and prevents thundering herd on rapid updates. |
| Finalizer-based cleanup | Guarantees cloud storage resources are cleaned up when the Config CR is deleted. Prevents orphaned buckets/containers. |
| Direct API reads for Config CR | Avoids informer cache staleness when the Config CR is patched in quick succession. |
| Immutable storage config | Storage type and location are set at bootstrap. Prevents accidental data loss from storage migrations. |
| Recreate strategy for PVC | RollingUpdate with PVC causes new pods to block on exclusive volume access. Cloud storage uses RollingUpdate. |
| Platform auto-detection | Reads Infrastructure CR to select the right storage driver without user configuration. |

## Testing

- **Unit tests**: Colocated with source in `pkg/`. Mock cloud APIs and fake Kubernetes clients.
- **E2E tests**: In `test/e2e/`, require a real OpenShift cluster. Use the OTE framework with `[Serial]` and `[Parallel]` labels.
- **Test framework helpers**: In `test/framework/` for common setup and assertions.
