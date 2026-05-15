# ODH Observability Operator

Standalone monitoring module operator for Open Data Hub. Manages metrics, tracing, dashboards, and alerting via a cluster-scoped `Monitoring` singleton CR.

## Build and Test

```bash
make test          # Full test suite (codegen + fmt + vet + tests)
make unit-test     # Unit tests only
go build ./...     # Build all packages
```

## Architecture

- **Controller pattern**: Single reconciler (`MonitoringReconciler`) processes the `default-monitoring` CR
- **Resource deployment**: Template-driven SSA via `odh-platform-utilities/pkg/render/template` and `odh-platform-utilities/pkg/deploy`
- **Conditions**: `ConditionsManager` in `internal/controller/conditions/` manages per-feature conditions that aggregate into Ready/Degraded
- **Garbage collection**: Uses `odh-platform-utilities/pkg/controller/gc` with API discovery to remove stale resources
- **Webhook**: Mutating admission webhook injects monitoring labels on ServiceMonitors/PodMonitors

## Key Directories

- `api/v1alpha1/` — CRD type definitions (MonitoringSpec, Metrics, Traces, etc.)
- `internal/controller/` — Reconciliation logic (actions.go, monitoring_reconciler.go, templatedata.go, helpers.go)
- `internal/controller/resources/` — Go-embedded YAML templates rendered at runtime
- `internal/controller/conditions/` — Condition type constants and aggregation logic
- `internal/webhook/` — Mutating admission webhook handler
- `charts/odh-observability/` — Helm chart for deploying the operator itself
- `cmd/main.go` — Entrypoint, manager setup

## Testing Conventions

- Standard Go `testing` package (no testify, gomega, ginkgo)
- Table-driven tests with subtests
- Fake clients from `sigs.k8s.io/controller-runtime/pkg/client/fake`
- Test helpers: `newTestScheme()`, `newTestReconciler()`, `newMonitoring()` in `monitoring_reconciler_test.go`
- Interceptors from `sigs.k8s.io/controller-runtime/pkg/client/interceptor` for error injection

## Important Patterns

- **Singleton CR**: Name must be `default-monitoring` (enforced by CEL validation)
- **Cluster-scoped CR managing namespace-scoped resources**: Uses label-based Watches (not OwnerReferences) for drift detection
- **Namespace field is immutable**: CEL rule `self == oldSelf` prevents changes after creation
- **Finalizer**: `monitoring.opendatahub.io/cleanup` ensures resource cleanup on CR deletion
- **Feature degradation**: Missing CRDs result in `MarkNotConfigured` (Info severity) or `MarkFalse` depending on whether the feature was requested
