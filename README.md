# ODH Observability Operator

Standalone monitoring module operator for [Open Data Hub](https://opendatahub.io/). Manages a complete observability stack -- metrics, tracing, dashboards, and alerting -- via a single cluster-scoped `Monitoring` custom resource.

## Overview

The operator deploys and reconciles:

- **MonitoringStack + ThanosQuerier** (via Cluster Observability Operator) for metrics collection and multi-tenant querying
- **Tempo** (via Tempo Operator) for distributed tracing
- **OpenTelemetry Collector** for a unified telemetry pipeline
- **Perses** for dashboards and datasources
- **PrometheusRules** for alerting
- **Mutating webhook** for automatic monitoring label injection on ServiceMonitors and PodMonitors in opted-in namespaces

## Architecture

- **Cluster-scoped singleton CR**: A single `Monitoring` resource named `default-monitoring` targets a namespace (default: `opendatahub`). CEL validation enforces the singleton constraint.
- **Server-Side Apply (SSA)**: The controller deploys all child resources using SSA with field ownership, enabling clean conflict resolution and declarative resource management.
- **Label-based watches**: Since cluster-scoped CRs cannot own namespace-scoped resources via OwnerReferences, the operator uses label-based watches for drift detection and reconciliation.
- **Garbage collection via API discovery**: Stale resources are removed by discovering deployed resources through the API and comparing them to the desired state.
- **Webhook label injection**: A mutating admission webhook injects `platform.opendatahub.io/part-of: monitoring` labels on ServiceMonitors and PodMonitors in opted-in namespaces, ensuring they are automatically picked up by the MonitoringStack.

## Prerequisites

**Required:**

- OpenShift cluster
- cert-manager (for webhook TLS certificate management)

**Optional** (features degrade gracefully if absent):

- [Cluster Observability Operator](https://docs.openshift.com/container-platform/latest/observability/cluster_observability_operator/cluster-observability-operator-overview.html) -- for metrics (MonitoringStack, ThanosQuerier)
- [Tempo Operator](https://github.com/grafana/tempo-operator) -- for distributed tracing
- [OpenTelemetry Operator](https://github.com/open-telemetry/opentelemetry-operator) -- for the telemetry collector
- [Perses Operator](https://github.com/perses/perses-operator) -- for dashboards and datasources

## Quick Start

Install the operator via Helm:

```bash
helm install odh-observability charts/odh-observability \
  -n opendatahub-operator-system \
  --create-namespace
```

Then create a `Monitoring` CR:

```yaml
apiVersion: services.platform.opendatahub.io/v1alpha1
kind: Monitoring
metadata:
  name: default-monitoring
spec:
  namespace: opendatahub
  metrics:
    storage:
      size: 5Gi
      retention: 7d
  traces:
    storage:
      backend: pv
      size: 10Gi
    sampleRatio: "0.1"
```

Apply it:

```bash
kubectl apply -f monitoring.yaml
```

## Configuration Reference

All configuration lives under `spec` of the `Monitoring` CR.

| Field | Type | Description |
|---|---|---|
| `managementState` | `Managed` / `Removed` | Controls whether the operator actively manages the module or removes it. Default: `Managed`. |
| `namespace` | string | Target namespace for monitoring resources. Default: `opendatahub`. Immutable after creation. |
| `metrics.storage.size` | quantity | PVC storage size for metrics (e.g. `5Gi`). |
| `metrics.storage.retention` | string | How long metrics data is retained (e.g. `7d`, `2w`). |
| `metrics.replicas` | int | Number of MonitoringStack replicas. Requires `metrics.storage`. |
| `metrics.exporters` | map | Custom metrics exporters in OpenTelemetry Collector format. Reserved names `prometheus` and `otlp/tempo` are not allowed. Max 10. |
| `traces.storage.backend` | `pv` / `s3` / `gcs` | Storage backend for Tempo. |
| `traces.storage.size` | string | Storage size (PV backend only). |
| `traces.storage.secret` | string | Secret name with storage credentials (required for `s3` and `gcs` backends). |
| `traces.storage.retention` | duration | How long trace data is retained (e.g. `60m`, `10h`). |
| `traces.sampleRatio` | string | Trace sampling rate from `0.0` to `1.0`. |
| `traces.tls.enabled` | bool | Enable TLS for Tempo OTLP ingestion and query APIs. |
| `traces.tls.certificateSecret` | string | Secret containing TLS certificates. |
| `traces.tls.caConfigMap` | string | ConfigMap containing the CA certificate. |
| `traces.exporters` | map | Custom trace exporters for external observability tools. |
| `alerting` | object | Enables Prometheus alerting rules. Requires `metrics.storage` to be configured. |
| `collectorReplicas` | int | Number of OpenTelemetry Collector replicas. Defaults to 1 on single-node clusters, 2 on multi-node. Requires `metrics.storage` or `traces` to be configured. |

## Development

### Build and Test

```bash
make test          # Run tests with codegen (manifests, deepcopy, fmt, vet)
make unit-test     # Run unit tests only (no codegen prerequisites)
make test-verbose  # Run unit tests with verbose output
go build ./...     # Build
make build         # Build manager binary (with codegen)
make run           # Run locally against cluster
```

### Helm Chart

```bash
make helm-update-crds   # Sync generated CRDs into the Helm chart
make helm-lint          # Lint the Helm chart
make helm-template      # Render Helm chart templates
```

### Docker

```bash
make docker-build   # Build container image
make docker-push    # Push container image
```

### Project Structure

```
api/v1alpha1/                      -- CRD types and deepcopy
internal/controller/               -- Reconciliation logic
internal/controller/resources/     -- Template manifests (Go templates)
internal/controller/conditions/    -- Status condition helpers
internal/controller/gvk/           -- GroupVersionKind definitions
internal/webhook/                  -- Mutating admission webhook
charts/odh-observability/          -- Helm chart for operator deployment
cmd/                               -- Operator entrypoint
config/                            -- Kustomize manifests (CRDs, RBAC, webhook)
hack/                              -- Code generation boilerplate
```

## Integration with ODH Operator

This operator can run **standalone** or as a **module managed by the ODH operator**. When managed as a module, the ODH operator's module handler deploys the Helm chart and creates/manages the `Monitoring` CR on behalf of the platform. The operator's API types implement the `PlatformObject` interface from `odh-platform-utilities` to support this integration.

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.
