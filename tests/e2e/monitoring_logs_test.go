package e2e_test

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/odh-observability/internal/controller/conditions"
	"github.com/opendatahub-io/odh-observability/internal/controller/gvk"
	jq "github.com/opendatahub-io/odh-observability/tests/e2e/matchers/jq"
	common "github.com/opendatahub-io/odh-platform-utilities/api/common"

	. "github.com/onsi/gomega"
)

// ========================================================================
// Group 11: Logs Collection
// ========================================================================

func (tc *MonitoringTestCtx) runLogsCollectionTests(t *testing.T) {
	t.Helper()

	t.Run("Group 11: Logs Collection", func(t *testing.T) {
		t.Cleanup(func() {
			tc.cleanupGroup(t, "")
		})

		t.Run("Test Logs Collector not deployed without logs config", tc.ValidateLogsCollectorNotDeployedWithoutConfig)
		t.Run("Test Logs Collector deployment with logs config", tc.ValidateLogsCollectorDeployment)
		t.Run("Test Logs Collector configuration", tc.ValidateLogsCollectorConfiguration)
		t.Run("Test Logs Collector RBAC configuration", tc.ValidateLogsCollectorRBACConfiguration)
		t.Run("Test Logs Collector lifecycle", tc.ValidateLogsCollectorLifecycle)
	})
}

// ValidateLogsCollectorNotDeployedWithoutConfig tests that the logs collector is not deployed when logs are not configured.
func (tc *MonitoringTestCtx) ValidateLogsCollectorNotDeployedWithoutConfig(t *testing.T) {
	t.Helper()
	t.Cleanup(tc.resetMonitoringConfigToManaged)

	tc.updateMonitoringConfig(
		withManagementState(common.Managed),
		withNoLogs(),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: tc.MonitoringCRName}),
		WithCondition(And(
			jq.Match(`.spec.logs == null`),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, common.ConditionTypeReady, metav1.ConditionTrue),
		)),
		WithCustomErrorMsg("Monitoring resource should be created without logs configuration"),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: tc.MonitoringCRName}),
		WithCondition(jq.Match(
			`[.status.conditions[] | select(.type=="%s" and .status=="False")] | length==1`,
			conditions.ConditionLogsCollectorAvailable,
		)),
		WithCustomErrorMsg("LogsCollectorAvailable condition should be False when logs are not configured"),
	)

	tc.EnsureResourceGone(
		WithMinimalObject(gvk.OpenTelemetryCollector, types.NamespacedName{
			Name:      LogsCollectorName,
			Namespace: tc.MonitoringNamespace,
		}),
	)
}

// ValidateLogsCollectorDeployment tests that the logs collector is deployed and ready when logs are configured.
func (tc *MonitoringTestCtx) ValidateLogsCollectorDeployment(t *testing.T) {
	t.Helper()
	t.Cleanup(tc.resetMonitoringConfigToManaged)

	lokiEndpoint := "https://loki-gateway.loki.svc.cluster.local:8080/api/logs/v1/application"

	tc.updateMonitoringConfig(
		withManagementState(common.Managed),
		withLogsConfig(lokiEndpoint),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: tc.MonitoringCRName}),
		WithCondition(And(
			jq.Match(`.spec.logs.endpoint == "%s"`, lokiEndpoint),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, common.ConditionTypeReady, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, conditions.ConditionLogsCollectorAvailable, metav1.ConditionTrue),
		)),
		WithCustomErrorMsg("Monitoring resource should be updated with logs configuration and LogsCollector should be available"),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.OpenTelemetryCollector, types.NamespacedName{
			Name:      LogsCollectorName,
			Namespace: tc.MonitoringNamespace,
		}),
		WithCondition(And(
			jq.Match(`.spec.mode == "deployment"`),
			jq.Match(`.spec.replicas == 2`),
			monitoringOwnerReferencesCondition,
		)),
		WithCustomErrorMsg("Logs OpenTelemetryCollector should be created in deployment mode with 2 replicas"),
	)

	tc.EnsureDeploymentReady(
		WithMinimalObject(gvk.Deployment, types.NamespacedName{
			Name:      LogsCollectorName + "-collector",
			Namespace: tc.MonitoringNamespace,
		}),
	)
}

// ValidateLogsCollectorConfiguration validates the logs collector configuration details.
func (tc *MonitoringTestCtx) ValidateLogsCollectorConfiguration(t *testing.T) {
	t.Helper()
	t.Cleanup(tc.resetMonitoringConfigToManaged)

	lokiEndpoint := "https://loki-gateway.loki.svc.cluster.local:8080/api/logs/v1/application"

	tc.updateMonitoringConfig(
		withManagementState(common.Managed),
		withLogsConfig(lokiEndpoint),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.OpenTelemetryCollector, types.NamespacedName{
			Name:      LogsCollectorName,
			Namespace: tc.MonitoringNamespace,
		}),
		WithCondition(And(
			// Verify receivers
			jq.Match(`.spec.config.receivers.otlp.protocols.grpc.endpoint == "0.0.0.0:4317"`),
			jq.Match(`.spec.config.receivers.otlp.protocols.http.endpoint == "0.0.0.0:4318"`),

			// Verify processors
			jq.Match(`.spec.config.processors.k8sattributes != null`),
			jq.Match(`.spec.config.processors.k8sattributes.auth_type == "serviceAccount"`),
			jq.Match(`.spec.config.processors."groupbyattrs/maas" != null`),
			jq.Match(`.spec.config.processors.batch != null`),

			// Verify exporter endpoint
			jq.Match(`.spec.config.exporters."otlphttp/loki".endpoint == "%s"`, lokiEndpoint),
			jq.Match(`.spec.config.exporters."otlphttp/loki".tls.ca_file == "/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt"`),
			jq.Match(`.spec.config.exporters."otlphttp/loki".auth.authenticator == "bearertokenauth"`),
			jq.Match(`.spec.config.exporters."otlphttp/loki".headers."X-Scope-OrgID" == "application"`),

			// Verify pipeline
			jq.Match(`.spec.config.service.pipelines.logs.receivers | contains(["otlp"])`),
			jq.Match(`.spec.config.service.pipelines.logs.processors | contains(["resource", "k8sattributes", "groupbyattrs/maas", "batch"])`),
			jq.Match(`.spec.config.service.pipelines.logs.exporters | contains(["otlphttp/loki"])`),
		)),
		WithCustomErrorMsg("Logs collector should have correct OTLP receivers, processors, and Loki exporter configuration"),
	)
}

// ValidateLogsCollectorRBACConfiguration tests that the logs collector has correct RBAC permissions.
func (tc *MonitoringTestCtx) ValidateLogsCollectorRBACConfiguration(t *testing.T) {
	t.Helper()
	t.Cleanup(tc.resetMonitoringConfigToManaged)

	lokiEndpoint := "https://loki-gateway.loki.svc.cluster.local:8080/api/logs/v1/application"

	tc.updateMonitoringConfig(
		withManagementState(common.Managed),
		withLogsConfig(lokiEndpoint),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ServiceAccount, types.NamespacedName{
			Name:      LogsCollectorServiceAccount,
			Namespace: tc.MonitoringNamespace,
		}),
		WithCustomErrorMsg("ServiceAccount for logs collector should exist"),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ClusterRole, types.NamespacedName{
			Name: LogsCollectorName + "-processor",
		}),
		WithCondition(And(
			// k8sattributes processor requires pod/namespace metadata access
			jq.Match(`.rules[] | select(.apiGroups[] == "") | .resources | contains(["pods", "namespaces"])`),
			jq.Match(`.rules[] | select(.apiGroups[] == "") | .verbs | contains(["get", "watch", "list"])`),
			jq.Match(`.rules[] | select(.apiGroups[] == "apps") | .resources | contains(["replicasets"])`),
			jq.Match(`.rules[] | select(.apiGroups[] == "apps") | .verbs | contains(["get", "watch", "list"])`),
		)),
		WithCustomErrorMsg("ClusterRole should grant logs collector permissions for k8sattributes processor"),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ClusterRoleBinding, types.NamespacedName{
			Name: LogsCollectorName + "-processor",
		}),
		WithCondition(And(
			jq.Match(`.roleRef.name == "%s"`, LogsCollectorName+"-processor"),
			jq.Match(`.subjects[0].name == "%s"`, LogsCollectorServiceAccount),
			jq.Match(`.subjects[0].namespace == "%s"`, tc.MonitoringNamespace),
		)),
		WithCustomErrorMsg("ClusterRoleBinding should bind logs collector ClusterRole to ServiceAccount"),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ClusterRoleBinding, types.NamespacedName{
			Name: LogsCollectorName + "-loki-writer",
		}),
		WithCondition(And(
			jq.Match(`.roleRef.name == "lokistack-application-logs-writer"`),
			jq.Match(`.subjects[0].name == "%s"`, LogsCollectorServiceAccount),
			jq.Match(`.subjects[0].namespace == "%s"`, tc.MonitoringNamespace),
		)),
		WithCustomErrorMsg("ClusterRoleBinding should bind lokistack-application-logs-writer role to logs collector ServiceAccount"),
	)
}

// ValidateLogsCollectorLifecycle tests the complete lifecycle of logs collector deployment and cleanup.
func (tc *MonitoringTestCtx) ValidateLogsCollectorLifecycle(t *testing.T) {
	t.Helper()
	t.Cleanup(tc.resetMonitoringConfigToManaged)

	lokiEndpoint := "https://loki-gateway.loki.svc.cluster.local:8080/api/logs/v1/application"

	// Step 1: Enable logs
	tc.updateMonitoringConfig(
		withManagementState(common.Managed),
		withLogsConfig(lokiEndpoint),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.OpenTelemetryCollector, types.NamespacedName{
			Name:      LogsCollectorName,
			Namespace: tc.MonitoringNamespace,
		}),
		WithCondition(jq.Match(`.spec.config.exporters."otlphttp/loki" != null`)),
		WithCustomErrorMsg("Logs collector should be deployed when logs are enabled"),
	)

	// Step 2: Disable logs
	tc.updateMonitoringConfig(
		withManagementState(common.Managed),
		withNoLogs(),
	)

	tc.EnsureResourceGone(
		WithMinimalObject(gvk.OpenTelemetryCollector, types.NamespacedName{
			Name:      LogsCollectorName,
			Namespace: tc.MonitoringNamespace,
		}),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: tc.MonitoringCRName}),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, conditions.ConditionLogsCollectorAvailable, metav1.ConditionFalse),
		),
		WithCustomErrorMsg("LogsCollectorAvailable condition should be False when logs are disabled"),
	)

	// Step 3: Re-enable logs
	tc.updateMonitoringConfig(
		withManagementState(common.Managed),
		withLogsConfig(lokiEndpoint),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.OpenTelemetryCollector, types.NamespacedName{
			Name:      LogsCollectorName,
			Namespace: tc.MonitoringNamespace,
		}),
		WithCondition(jq.Match(`.spec.config.exporters."otlphttp/loki" != null`)),
		WithCustomErrorMsg("Logs collector should be recreated when logs are re-enabled"),
	)
}
