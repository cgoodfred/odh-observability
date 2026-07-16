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
// Group 11: Usage Logs Collection
// ========================================================================

func (tc *MonitoringTestCtx) runUsageLogsCollectionTests(t *testing.T) {
	t.Helper()

	t.Run("Group 11: Usage Logs Collection", func(t *testing.T) {
		t.Cleanup(func() {
			tc.cleanupGroup(t, "")
		})

		t.Run("Test Usage Logs Collector not deployed without usage logs config", tc.ValidateUsageUsageLogsCollectorNotDeployedWithoutConfig)
		t.Run("Test Usage Logs Collector deployment with usage logs config", tc.ValidateUsageUsageLogsCollectorDeployment)
		t.Run("Test Usage Logs Collector configuration", tc.ValidateUsageUsageLogsCollectorConfiguration)
		t.Run("Test Usage Logs Collector RBAC configuration", tc.ValidateUsageUsageLogsCollectorRBACConfiguration)
		t.Run("Test Usage Logs Collector lifecycle", tc.ValidateUsageUsageLogsCollectorLifecycle)
	})
}

// ValidateUsageUsageLogsCollectorNotDeployedWithoutConfig tests that the logs collector is not deployed when logs are not configured.
func (tc *MonitoringTestCtx) ValidateUsageUsageLogsCollectorNotDeployedWithoutConfig(t *testing.T) {
	t.Helper()
	t.Cleanup(tc.resetMonitoringConfigToManaged)

	tc.updateMonitoringConfig(
		withManagementState(common.Managed),
		withNoUsageLogs(),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: tc.MonitoringCRName}),
		WithCondition(And(
			jq.Match(`.spec.usageLogs == null`),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, common.ConditionTypeReady, metav1.ConditionTrue),
		)),
		WithCustomErrorMsg("Monitoring resource should be created without logs configuration"),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: tc.MonitoringCRName}),
		WithCondition(jq.Match(
			`[.status.conditions[] | select(.type=="%s" and .status=="False")] | length==1`,
			conditions.ConditionUsageLogsCollectorAvailable,
		)),
		WithCustomErrorMsg("UsageLogsCollectorAvailable condition should be False when logs are not configured"),
	)

	tc.EnsureResourceGone(
		WithMinimalObject(gvk.OpenTelemetryCollector, types.NamespacedName{
			Name:      UsageLogsCollectorName,
			Namespace: tc.MonitoringNamespace,
		}),
	)
}

// ValidateUsageUsageLogsCollectorDeployment tests that the logs collector is deployed and ready when logs are configured.
func (tc *MonitoringTestCtx) ValidateUsageUsageLogsCollectorDeployment(t *testing.T) {
	t.Helper()
	t.Cleanup(tc.resetMonitoringConfigToManaged)

	lokiEndpoint := "https://loki-gateway.loki.svc.cluster.local:8080/api/logs/v1/application"

	tc.updateMonitoringConfig(
		withManagementState(common.Managed),
		withUsageLogsConfig(lokiEndpoint),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: tc.MonitoringCRName}),
		WithCondition(And(
			jq.Match(`.spec.usageLogs.endpoint == "%s"`, lokiEndpoint),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, common.ConditionTypeReady, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, conditions.ConditionUsageLogsCollectorAvailable, metav1.ConditionTrue),
		)),
		WithCustomErrorMsg("Monitoring resource should be updated with logs configuration and UsageLogsCollector should be available"),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.OpenTelemetryCollector, types.NamespacedName{
			Name:      UsageLogsCollectorName,
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
			Name:      UsageLogsCollectorName + "-collector",
			Namespace: tc.MonitoringNamespace,
		}),
	)
}

// ValidateUsageUsageLogsCollectorConfiguration validates the logs collector configuration details.
func (tc *MonitoringTestCtx) ValidateUsageUsageLogsCollectorConfiguration(t *testing.T) {
	t.Helper()
	t.Cleanup(tc.resetMonitoringConfigToManaged)

	lokiEndpoint := "https://loki-gateway.loki.svc.cluster.local:8080/api/logs/v1/application"

	tc.updateMonitoringConfig(
		withManagementState(common.Managed),
		withUsageLogsConfig(lokiEndpoint),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.OpenTelemetryCollector, types.NamespacedName{
			Name:      UsageLogsCollectorName,
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

// ValidateUsageUsageLogsCollectorRBACConfiguration tests that the logs collector has correct RBAC permissions.
func (tc *MonitoringTestCtx) ValidateUsageUsageLogsCollectorRBACConfiguration(t *testing.T) {
	t.Helper()
	t.Cleanup(tc.resetMonitoringConfigToManaged)

	lokiEndpoint := "https://loki-gateway.loki.svc.cluster.local:8080/api/logs/v1/application"

	tc.updateMonitoringConfig(
		withManagementState(common.Managed),
		withUsageLogsConfig(lokiEndpoint),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ServiceAccount, types.NamespacedName{
			Name:      UsageLogsCollectorServiceAccount,
			Namespace: tc.MonitoringNamespace,
		}),
		WithCustomErrorMsg("ServiceAccount for logs collector should exist"),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ClusterRole, types.NamespacedName{
			Name: UsageLogsCollectorName + "-processor",
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
			Name: UsageLogsCollectorName + "-processor",
		}),
		WithCondition(And(
			jq.Match(`.roleRef.name == "%s"`, UsageLogsCollectorName+"-processor"),
			jq.Match(`.subjects[0].name == "%s"`, UsageLogsCollectorServiceAccount),
			jq.Match(`.subjects[0].namespace == "%s"`, tc.MonitoringNamespace),
		)),
		WithCustomErrorMsg("ClusterRoleBinding should bind logs collector ClusterRole to ServiceAccount"),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.ClusterRoleBinding, types.NamespacedName{
			Name: UsageLogsCollectorName + "-loki-writer",
		}),
		WithCondition(And(
			jq.Match(`.roleRef.name == "lokistack-application-logs-writer"`),
			jq.Match(`.subjects[0].name == "%s"`, UsageLogsCollectorServiceAccount),
			jq.Match(`.subjects[0].namespace == "%s"`, tc.MonitoringNamespace),
		)),
		WithCustomErrorMsg("ClusterRoleBinding should bind lokistack-application-logs-writer role to logs collector ServiceAccount"),
	)
}

// ValidateUsageUsageLogsCollectorLifecycle tests the complete lifecycle of logs collector deployment and cleanup.
func (tc *MonitoringTestCtx) ValidateUsageUsageLogsCollectorLifecycle(t *testing.T) {
	t.Helper()
	t.Cleanup(tc.resetMonitoringConfigToManaged)

	lokiEndpoint := "https://loki-gateway.loki.svc.cluster.local:8080/api/logs/v1/application"

	// Step 1: Enable logs
	tc.updateMonitoringConfig(
		withManagementState(common.Managed),
		withUsageLogsConfig(lokiEndpoint),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.OpenTelemetryCollector, types.NamespacedName{
			Name:      UsageLogsCollectorName,
			Namespace: tc.MonitoringNamespace,
		}),
		WithCondition(jq.Match(`.spec.config.exporters."otlphttp/loki" != null`)),
		WithCustomErrorMsg("Logs collector should be deployed when logs are enabled"),
	)

	// Step 2: Disable logs
	tc.updateMonitoringConfig(
		withManagementState(common.Managed),
		withNoUsageLogs(),
	)

	tc.EnsureResourceGone(
		WithMinimalObject(gvk.OpenTelemetryCollector, types.NamespacedName{
			Name:      UsageLogsCollectorName,
			Namespace: tc.MonitoringNamespace,
		}),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.Monitoring, types.NamespacedName{Name: tc.MonitoringCRName}),
		WithCondition(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, conditions.ConditionUsageLogsCollectorAvailable, metav1.ConditionFalse),
		),
		WithCustomErrorMsg("UsageLogsCollectorAvailable condition should be False when logs are disabled"),
	)

	// Step 3: Re-enable logs
	tc.updateMonitoringConfig(
		withManagementState(common.Managed),
		withUsageLogsConfig(lokiEndpoint),
	)

	tc.EnsureResourceExists(
		WithMinimalObject(gvk.OpenTelemetryCollector, types.NamespacedName{
			Name:      UsageLogsCollectorName,
			Namespace: tc.MonitoringNamespace,
		}),
		WithCondition(jq.Match(`.spec.config.exporters."otlphttp/loki" != null`)),
		WithCustomErrorMsg("Logs collector should be recreated when logs are re-enabled"),
	)
}
