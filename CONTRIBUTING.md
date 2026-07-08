# Contributing to ODH Observability Operator

## Prerequisites

- Go 1.25+
- kubectl
- Helm 3
- Access to a Kubernetes or OpenShift cluster (for E2E testing)
- [Podman](https://podman.io/) or Docker (for container builds)

## Development Workflow

1. Fork and clone the repository.
2. Create a feature branch from `main`.
3. Make your changes.
4. Run tests and linting:
   ```bash
   make test
   make lint
   make helm-lint
   ```
5. Open a pull request against `main`.

## Commit Messages

This project uses [Conventional Commits](https://www.conventionalcommits.org/). Each commit message should follow the format:

```
type(scope): description
```

Common types: `feat`, `fix`, `refactor`, `docs`, `test`, `chore`.

Examples:
```
feat(traces): add GCS storage backend support
fix(webhook): handle missing namespace label gracefully
docs: update configuration reference in README
```

## Running Tests

```bash
# Full test suite (includes codegen, fmt, vet)
make test

# Unit tests only (faster, skips codegen)
make unit-test

# E2E tests (requires a cluster with KUBECONFIG set)
make e2e-test
```

## Code Generation

After modifying API types in `api/v1alpha1/`:

```bash
# Regenerate CRD manifests, RBAC, webhook configs, and DeepCopy
make manifests generate

# Sync CRDs to the Helm chart
make helm-update-crds
```

Always commit generated files in a separate commit from your source changes so reviewers can distinguish hand-written code from generated output.

## Helm Chart

The Helm chart is in `charts/odh-observability/`. After modifying the chart:

```bash
# Lint
make helm-lint

# Render templates to verify output
make helm-template
```

## Code Style

- Run `make fmt` before committing.
- Run `make vet` to catch common issues.
- Run `make lint` for the full golangci-lint suite.
- All Go source files must include the Apache 2.0 license header (see `hack/boilerplate.go.txt`).
