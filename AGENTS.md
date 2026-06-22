# AI Agent Instructions for cluster-image-registry-operator

Before making changes, check the `docs/` directory for additional context — it contains documentation on credentials flow, development builds, operator lifecycle, and metrics.

## What This Repo Is

This is an OpenShift operator that manages the singleton internal container image registry. It handles registry deployment, storage provisioning across cloud providers (AWS S3, Azure Blob, GCS, IBM COS, OpenStack Swift, PVC, EmptyDir), routes, and image pruning.

The operator runs in the `openshift-image-registry` namespace and is managed by the ClusterVersionOperator (CVO).

## Critical Rules

1. **Do not edit vendored files.** The `vendor/` directory is managed by `go mod tidy && go mod vendor`. Never hand-edit anything under `vendor/`.
2. **Do not edit generated files.** Files matching `zz_generated.*` and CRD manifests under `vendor/github.com/openshift/api/` are generated in the `openshift/api` repo.
3. **Use `make build`, not `go build`.** The Makefile injects version info via ldflags and places binaries in `tmp/_output/bin/`.
4. **Run `make verify` before considering any change complete.** This runs gofmt, golangci-lint, and dependency checks.

## Repository Structure

```
cmd/
├── cluster-image-registry-operator/         # Main operator binary
├── cluster-image-registry-operator-tests-ext/ # OTE e2e test harness
└── move-blobs/                              # Azure blob migration utility

pkg/
├── operator/           # Controllers (11+ running concurrently)
│   ├── controller.go   # Main reconciliation controller
│   ├── starter.go      # Wires and starts all controllers
│   ├── bootstrap.go    # Creates default Config CR with platform defaults
│   └── ...             # ClusterOperator status, pruner, certs, metrics, etc.
├── resource/           # Kubernetes resource generators (Deployment, Service, Route, RBAC, etc.)
├── storage/            # Cloud storage drivers (s3/, azure/, gcs/, ibmcos/, swift/, pvc/, emptydir/)
├── client/             # Custom client/lister wrappers
├── defaults/           # Constants (namespace names, resource names)
└── metrics/            # Prometheus metrics

test/
├── e2e/                # End-to-end tests (require real OpenShift cluster)
└── framework/          # Shared test helpers
```

## Key Patterns to Follow

- **Storage Driver interface**: All cloud backends implement the `Driver` interface in `pkg/storage/`. New storage backends must implement the full interface — no partial implementations.
- **Single workqueue**: All events coalesce to a single workqueue key (`"changes"`). The main controller's `sync()` handles all reconciliation in one pass.
- **Config CR is source of truth**: All reconciliation state comes from the `configs.imageregistry.operator.openshift.io/cluster` CR. Do not derive state from Deployment status alone.
- **Retry on conflict**: Use `retry.RetryOnConflict` when updating the Config CR status — multiple controllers may update it concurrently.

## Important Constraints

- **Storage config is immutable after bootstrap.** S3 bucket names, Azure container names, etc. are set once during initial Config CR creation and are not changed afterward. Changing storage type requires CR deletion and recreation.
- **Config CR is read directly from the API server**, not from the informer cache, to avoid staleness issues during rapid updates.
- **PVC storage forces `Recreate` deployment strategy** because `RollingUpdate` causes new pods to block waiting for exclusive volume access. Cloud storage backends use `RollingUpdate`.
- **Platform detection** reads `config.openshift.io/infrastructures/cluster`. Unknown platforms default to EmptyDir (ephemeral) storage.
- **Credential override**: Users can supply custom cloud credentials via the `image-registry-private-configuration-user` secret. The operator merges these into `image-registry-private-configuration` for the storage drivers.

## Build and Test

```bash
make build       # Compile operator + test binaries
make test-unit   # Unit tests
make test-e2e    # E2E tests (requires real OpenShift cluster)
make verify      # Linters, gofmt, dependency checks
```

### OTE (OpenShift Tests Extension)

E2E tests run via the OTE framework. The test harness binary is at `cmd/cluster-image-registry-operator-tests-ext/` and registers two suites:

- **Serial suite** (`openshift/cluster-image-registry-operator/operator/serial`): Parallelism=1. Runs tests tagged `[Serial]` or `[Disruptive]` — these modify shared cluster state (e.g., ImageRegistry config, storage).
- **Parallel suite** (`openshift/cluster-image-registry-operator/operator/parallel`): Parallelism=4. Runs all other tests concurrently.

Tests live in `test/e2e/` and use the shared `test/framework/` helpers. They require a real OpenShift cluster — do not run on Kind or Minikube. When adding new e2e tests, tag with `[Serial]` if the test modifies cluster-wide resources; otherwise it runs in the parallel suite by default.

## What NOT to Do

- Do not modify CRD definitions here — they live in the `openshift/api` repo.
- Do not modify OWNERS or OWNERS_ALIASES files.
- Do not modify TLS certificate management code — this functionality is being removed from the operator.
- Do not assume cloud credentials are available outside of secrets. The operator reads credentials from `image-registry-private-configuration` (and user overrides from `image-registry-private-configuration-user`) to configure storage drivers.
- Do not add cloud provider SDK calls without ensuring the corresponding storage driver handles credential refresh and error retries.
- Do not run e2e tests on Kind or Minikube — they require a full OpenShift cluster.
