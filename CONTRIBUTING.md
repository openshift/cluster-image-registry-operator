# Contributing to cluster-image-registry-operator

The cluster-image-registry-operator manages the singleton OpenShift internal container image registry. It handles registry deployment, multi-cloud storage provisioning, TLS certificates, routes, and image pruning.

## Development Workflow

1. Fork the repo and clone your fork.
2. Create a feature branch from `master`.
3. Make your changes, add or update tests.
4. Run verification locally before pushing:

```bash
make build       # Compile operator binary (output: tmp/_output/bin/)
make test-unit   # Run unit tests
make verify      # Run gofmt, golangci-lint, dependency checks
```

5. If you changed dependencies, update the vendor directory:

```bash
go mod tidy && go mod vendor
```

The vendor directory is checked in. CI will fail if vendor is stale.

6. Push your branch and open a PR against `openshift/cluster-image-registry-operator:master`.

## Pull Request Guidelines

- Keep PRs focused. One logical change per PR.
- Write clear commit messages. Reference Jira tickets where applicable (e.g., `OCPBUGS-12345: fix S3 bucket tagging`).
- Include unit tests for new functionality.
- PRs require approval from at least one approver listed in the `OWNERS` file.
- Do not modify `OWNERS` or `OWNERS_ALIASES` files without explicit direction.

## PR Review Rules

- All PRs require `/lgtm` from a reviewer and `/approve` from an approver (OWNERS file). These are separate roles — the approver confirms the change belongs in the repo, the reviewer confirms correctness.
- Prow enforces required labels: `lgtm` and `approved` must both be present before merge.
- CI checks (`make verify`, `make test-unit`) must pass. Do not `/lgtm` a PR with failing CI.
- Changes to storage drivers (`pkg/storage/`) should be reviewed by someone familiar with the target cloud platform.
- Changes that affect the main reconciliation loop (`pkg/operator/controller.go`) or controller wiring (`pkg/operator/starter.go`) need careful review — all controllers share a single workqueue and these are high-impact paths.
- Do not approve PRs that modify vendored files, generated files (`zz_generated.*`), or CRD manifests — these are managed upstream.
- E2E test changes should be validated on a real OpenShift cluster before approval. Confirm `[Serial]` / `[Parallel]` tags are applied correctly.

## Testing

| Command | What it runs |
|---------|-------------|
| `make build` | Compile operator + test binaries |
| `make test-unit` | Unit tests across `./cmd/...`, `./pkg/...`, and `./test/framework/...` |
| `make test-e2e` | E2E tests (requires a real OpenShift cluster) |
| `make verify` | Linters, gofmt, dependency checks |

E2E tests use the OTE (OpenShift Tests Extension) framework and require a running OpenShift cluster. Tests are labeled `[Serial]` or `[Parallel]` — respect these labels. Do not run e2e tests on Kind or Minikube.

### Testing on an OpenShift Cluster

To test your operator image on a live cluster:

1. Patch the cluster version to allow your own image:

```bash
oc patch clusterversion/version --patch '{"spec":{"overrides":[{"kind":"Deployment", "name":"cluster-image-registry-operator","namespace":"openshift-image-registry","unmanaged":true}]}}' --type=merge
```

2. Build and push your image:

```bash
make build-image IMAGE=<MYREPO>/<MYIMAGE> TAG=<MYTAG>
```

3. Patch the Deployment to use your image:

```bash
oc patch deployment cluster-image-registry-operator -n openshift-image-registry --patch '{"spec":{"template":{"spec":{"containers":[{"name":"cluster-image-registry-operator","image":"<MYREPO>/<MYIMAGE>:<MYTAG>"}]}}}}' --type=strategic
```

## Code Conventions

- Follow standard Go conventions (gofmt, govet).
- Use existing patterns in the package you are modifying.
- Storage backends implement the `Driver` interface in `pkg/storage/`. New backends must implement the full interface.
- Use `retry.RetryOnConflict` when updating the Config CR status — multiple controllers may update it concurrently.

## Areas Requiring Extra Care

- **Storage drivers** (`pkg/storage/`): Each cloud backend has its own credential handling and error retry logic. Test with real cloud credentials when modifying.
- **CRD types**: CRD definitions live in the `openshift/api` repo, not here. Do not modify vendored CRD manifests.
- **Generated files**: Files matching `zz_generated.*` are generated upstream. Do not hand-edit.

## CI

CI runs via OpenShift's CI infrastructure (Prow / ci-operator). All `make verify` and `make test-unit` checks must pass for a PR to merge.
