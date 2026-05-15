/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"testing"

	platformcommon "github.com/opendatahub-io/odh-platform-utilities/api/common"
	libconditions "github.com/opendatahub-io/odh-platform-utilities/pkg/controller/conditions"
	rendertemplate "github.com/opendatahub-io/odh-platform-utilities/pkg/render/template"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	v1alpha1 "github.com/opendatahub-io/odh-observability/api/v1alpha1"
	"github.com/opendatahub-io/odh-observability/internal/controller/conditions"
	"github.com/opendatahub-io/odh-observability/internal/controller/gvk"
)

func newActionsTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := newTestScheme(t)
	return s
}

func registerCRDs(s *runtime.Scheme, gvks ...schema.GroupVersionKind) {
	for _, g := range gvks {
		s.AddKnownTypeWithName(g, &unstructured.Unstructured{})
		listGVK := schema.GroupVersionKind{
			Group:   g.Group,
			Version: g.Version,
			Kind:    g.Kind + "List",
		}
		s.AddKnownTypeWithName(listGVK, &unstructured.UnstructuredList{})
	}
}

func findCondition(m *v1alpha1.Monitoring, condType string) *platformcommon.Condition {
	return libconditions.FindStatusCondition(m, condType)
}

// --- deployMonitoringStackWithQuerierAndRestrictions ---

func TestDeployMonitoringStack_NoMetrics(t *testing.T) {
	s := newActionsTestScheme(t)
	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Spec.Metrics = nil

	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource

	err := deployMonitoringStackWithQuerierAndRestrictions(context.Background(),
		fake.NewClientBuilder().WithScheme(s).Build(), m, cm, &sources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sources) != 0 {
		t.Errorf("expected no sources when metrics is nil, got %d", len(sources))
	}

	msC := findCondition(m, conditions.ConditionMonitoringStackAvailable)
	if msC == nil || msC.Status != metav1.ConditionFalse || msC.Severity != platformcommon.ConditionSeverityInfo {
		t.Errorf("MonitoringStackAvailable: expected False+Info, got %v", msC)
	}

	tqC := findCondition(m, conditions.ConditionThanosQuerierAvailable)
	if tqC == nil || tqC.Status != metav1.ConditionFalse || tqC.Severity != platformcommon.ConditionSeverityInfo {
		t.Errorf("ThanosQuerierAvailable: expected False+Info, got %v", tqC)
	}
}

func TestDeployMonitoringStack_CRDsPresent(t *testing.T) {
	s := newActionsTestScheme(t)
	registerCRDs(s, gvk.MonitoringStack, gvk.ThanosQuerier)

	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Spec.Metrics = &v1alpha1.Metrics{}

	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource

	cli := fake.NewClientBuilder().WithScheme(s).Build()
	err := deployMonitoringStackWithQuerierAndRestrictions(context.Background(), cli, m, cm, &sources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sources) == 0 {
		t.Error("expected sources when CRDs are present")
	}

	msC := findCondition(m, conditions.ConditionMonitoringStackAvailable)
	if msC == nil || msC.Status != metav1.ConditionTrue {
		t.Error("MonitoringStackAvailable should be True")
	}
	tqC := findCondition(m, conditions.ConditionThanosQuerierAvailable)
	if tqC == nil || tqC.Status != metav1.ConditionTrue {
		t.Error("ThanosQuerierAvailable should be True")
	}
}

// --- deployTracingStack ---

func TestDeployTracingStack_NoTraces(t *testing.T) {
	s := newActionsTestScheme(t)
	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Spec.Traces = nil

	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource

	err := deployTracingStack(context.Background(),
		fake.NewClientBuilder().WithScheme(s).Build(), m, cm, &sources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sources) != 0 {
		t.Errorf("expected no sources, got %d", len(sources))
	}

	tempoC := findCondition(m, conditions.ConditionTempoAvailable)
	if tempoC == nil || tempoC.Status != metav1.ConditionFalse || tempoC.Severity != platformcommon.ConditionSeverityInfo {
		t.Errorf("TempoAvailable: expected False+Info, got %v", tempoC)
	}
}

func TestDeployTracingStack_PVBackend_CRDsPresent(t *testing.T) {
	s := newActionsTestScheme(t)
	registerCRDs(s, gvk.TempoMonolithic, gvk.Instrumentation)

	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Spec.Traces = &v1alpha1.Traces{
		Storage: v1alpha1.TracesStorage{Backend: v1alpha1.StorageBackendPV},
	}

	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource

	err := deployTracingStack(context.Background(),
		fake.NewClientBuilder().WithScheme(s).Build(), m, cm, &sources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sources) != 2 {
		t.Errorf("expected 2 sources (TempoMonolithic + Instrumentation), got %d", len(sources))
	}

	tempoC := findCondition(m, conditions.ConditionTempoAvailable)
	if tempoC == nil || tempoC.Status != metav1.ConditionTrue {
		t.Error("TempoAvailable should be True")
	}
}

func TestDeployTracingStack_S3Backend_CRDsPresent(t *testing.T) {
	s := newActionsTestScheme(t)
	registerCRDs(s, gvk.TempoStack, gvk.Instrumentation)

	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Spec.Traces = &v1alpha1.Traces{
		Storage: v1alpha1.TracesStorage{Backend: v1alpha1.StorageBackendS3, Secret: "my-secret"},
	}

	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource

	err := deployTracingStack(context.Background(),
		fake.NewClientBuilder().WithScheme(s).Build(), m, cm, &sources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sources) != 2 {
		t.Errorf("expected 2 sources (TempoStack + Instrumentation), got %d", len(sources))
	}
}

// --- deployOpenTelemetryCollector ---

func TestDeployOpenTelemetryCollector_NeitherMetricsNorTraces(t *testing.T) {
	s := newActionsTestScheme(t)
	m := newMonitoring(v1alpha1.MonitoringInstanceName)

	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource

	err := deployOpenTelemetryCollector(context.Background(),
		fake.NewClientBuilder().WithScheme(s).Build(), m, cm, &sources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	otcC := findCondition(m, conditions.ConditionOpenTelemetryCollectorAvailable)
	if otcC == nil || otcC.Status != metav1.ConditionFalse || otcC.Severity != platformcommon.ConditionSeverityInfo {
		t.Errorf("OTelCollectorAvailable: expected False+Info, got %v", otcC)
	}
}

func TestDeployOpenTelemetryCollector_MetricsOnly_CRDPresent(t *testing.T) {
	s := newActionsTestScheme(t)
	registerCRDs(s, gvk.OpenTelemetryCollector)

	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Spec.Metrics = &v1alpha1.Metrics{}

	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource

	err := deployOpenTelemetryCollector(context.Background(),
		fake.NewClientBuilder().WithScheme(s).Build(), m, cm, &sources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 3 base sources + 1 prometheus service (since metrics is configured)
	if len(sources) != 4 {
		t.Errorf("expected 4 sources for metrics+OTel, got %d", len(sources))
	}

	otcC := findCondition(m, conditions.ConditionOpenTelemetryCollectorAvailable)
	if otcC == nil || otcC.Status != metav1.ConditionTrue {
		t.Error("OTelCollectorAvailable should be True")
	}
}

func TestDeployOpenTelemetryCollector_TracesOnly_CRDPresent(t *testing.T) {
	s := newActionsTestScheme(t)
	registerCRDs(s, gvk.OpenTelemetryCollector)

	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Spec.Traces = &v1alpha1.Traces{
		Storage: v1alpha1.TracesStorage{Backend: v1alpha1.StorageBackendPV},
	}

	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource

	err := deployOpenTelemetryCollector(context.Background(),
		fake.NewClientBuilder().WithScheme(s).Build(), m, cm, &sources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 3 base sources only (no prometheus service since metrics nil)
	if len(sources) != 3 {
		t.Errorf("expected 3 sources for traces-only+OTel, got %d", len(sources))
	}
}

// --- deployAlerting ---

func TestDeployAlerting_NotConfigured(t *testing.T) {
	s := newActionsTestScheme(t)
	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Spec.Alerting = nil

	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource

	err := deployAlerting(context.Background(),
		fake.NewClientBuilder().WithScheme(s).Build(), m, cm, &sources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	alertC := findCondition(m, conditions.ConditionAlertingAvailable)
	if alertC == nil || alertC.Severity != platformcommon.ConditionSeverityInfo {
		t.Error("AlertingAvailable should be Info severity when not configured")
	}
}

func TestDeployAlerting_CRDPresent(t *testing.T) {
	s := newActionsTestScheme(t)
	registerCRDs(s, gvk.PrometheusRule)

	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Spec.Alerting = &v1alpha1.Alerting{}

	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource

	err := deployAlerting(context.Background(),
		fake.NewClientBuilder().WithScheme(s).Build(), m, cm, &sources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sources) != 1 {
		t.Errorf("expected 1 source for alerting, got %d", len(sources))
	}

	alertC := findCondition(m, conditions.ConditionAlertingAvailable)
	if alertC == nil || alertC.Status != metav1.ConditionTrue {
		t.Error("AlertingAvailable should be True")
	}
}

// --- deployNodeMetricsEndpoint ---

func TestDeployNodeMetricsEndpoint_NoMetrics(t *testing.T) {
	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource

	err := deployNodeMetricsEndpoint(context.Background(), nil, m, cm, &sources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sources) != 0 {
		t.Errorf("expected no sources, got %d", len(sources))
	}

	nodeC := findCondition(m, conditions.ConditionNodeMetricsEndpointAvailable)
	if nodeC == nil || nodeC.Severity != platformcommon.ConditionSeverityInfo {
		t.Error("NodeMetricsEndpointAvailable should be Info severity")
	}
}

func TestDeployNodeMetricsEndpoint_MetricsConfigured(t *testing.T) {
	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Spec.Metrics = &v1alpha1.Metrics{}
	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource

	err := deployNodeMetricsEndpoint(context.Background(), nil, m, cm, &sources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sources) != 1 {
		t.Errorf("expected 1 source, got %d", len(sources))
	}

	nodeC := findCondition(m, conditions.ConditionNodeMetricsEndpointAvailable)
	if nodeC == nil || nodeC.Status != metav1.ConditionTrue {
		t.Error("NodeMetricsEndpointAvailable should be True")
	}
}

// --- deployPerses ---

func TestDeployPerses_NoMetricsOrTraces(t *testing.T) {
	s := newActionsTestScheme(t)
	m := newMonitoring(v1alpha1.MonitoringInstanceName)

	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource

	err := deployPerses(context.Background(),
		fake.NewClientBuilder().WithScheme(s).Build(), m, cm, &sources, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	persesC := findCondition(m, conditions.ConditionPersesAvailable)
	if persesC == nil || persesC.Severity != platformcommon.ConditionSeverityInfo {
		t.Error("PersesAvailable should be Info severity when not configured")
	}
}

func TestDeployPerses_CRDNotFound(t *testing.T) {
	s := newActionsTestScheme(t)
	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Spec.Metrics = &v1alpha1.Metrics{}

	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource

	err := deployPerses(context.Background(),
		fake.NewClientBuilder().WithScheme(s).Build(), m, cm, &sources, "", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	persesC := findCondition(m, conditions.ConditionPersesAvailable)
	if persesC == nil || persesC.Status != metav1.ConditionFalse {
		t.Error("PersesAvailable should be False when CRD not found")
	}
}

func TestDeployPerses_CRDPresent(t *testing.T) {
	s := newActionsTestScheme(t)
	registerCRDs(s, gvk.PersesV1Alpha2)

	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Spec.Metrics = &v1alpha1.Metrics{}

	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource

	err := deployPerses(context.Background(),
		fake.NewClientBuilder().WithScheme(s).Build(), m, cm, &sources, "v1alpha2", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sources) != 2 {
		t.Errorf("expected 2 sources (Perses + NetworkPolicy), got %d", len(sources))
	}

	persesC := findCondition(m, conditions.ConditionPersesAvailable)
	if persesC == nil || persesC.Status != metav1.ConditionTrue {
		t.Error("PersesAvailable should be True")
	}
}

// --- deployMonitoringAdmissionPolicies ---

func TestDeployMonitoringAdmissionPolicies(t *testing.T) {
	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource

	err := deployMonitoringAdmissionPolicies(context.Background(), nil, m, cm, &sources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sources) != 1 {
		t.Errorf("expected 1 source for admission policies, got %d", len(sources))
	}
}

// --- deployPersesPrometheusIntegration ---

func TestDeployPersesPrometheusIntegration_NoMetrics(t *testing.T) {
	s := newActionsTestScheme(t)
	m := newMonitoring(v1alpha1.MonitoringInstanceName)

	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource

	err := deployPersesPrometheusIntegration(context.Background(),
		fake.NewClientBuilder().WithScheme(s).Build(), m, cm, &sources, "v1alpha2", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	c := findCondition(m, conditions.ConditionPersesPrometheusDataSourceAvailable)
	if c == nil || c.Severity != platformcommon.ConditionSeverityInfo {
		t.Error("expected Info severity when metrics not configured")
	}
}

func TestDeployPersesPrometheusIntegration_CRDPresent(t *testing.T) {
	s := newActionsTestScheme(t)
	registerCRDs(s, gvk.PersesDatasourceV1Alpha2)

	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Spec.Metrics = &v1alpha1.Metrics{}

	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource

	err := deployPersesPrometheusIntegration(context.Background(),
		fake.NewClientBuilder().WithScheme(s).Build(), m, cm, &sources, "v1alpha2", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sources) != 2 {
		t.Errorf("expected 2 sources (prometheus + cluster prometheus datasource), got %d", len(sources))
	}

	c := findCondition(m, conditions.ConditionPersesPrometheusDataSourceAvailable)
	if c == nil || c.Status != metav1.ConditionTrue {
		t.Error("PersesPrometheusDataSourceAvailable should be True")
	}
}

// --- ensureWebhookEnabled ---

func TestEnsureWebhookEnabled_AllComponentsPresent(t *testing.T) {
	s := newActionsTestScheme(t)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-operator", Namespace: "test-ns"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "manager",
						Args: []string{"--leader-elect", webhookArgEnabled},
						Ports: []corev1.ContainerPort{{
							Name:          "webhook",
							ContainerPort: webhookPort,
							Protocol:      corev1.ProtocolTCP,
						}},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      webhookVolumeName,
							MountPath: webhookCertMountPath,
							ReadOnly:  true,
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: webhookVolumeName,
						VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{
								SecretName: "test-operator-webhook-cert",
							},
						},
					}},
				},
			},
		},
	}

	cli := fake.NewClientBuilder().WithScheme(s).WithObjects(dep).Build()
	err := ensureWebhookEnabled(context.Background(), cli, "test-operator", "test-ns", "test-operator-webhook-cert")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify deployment was not modified (all components already present).
	got := &appsv1.Deployment{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: "test-operator", Namespace: "test-ns"}, got); err != nil {
		t.Fatalf("failed to get deployment: %v", err)
	}

	container := got.Spec.Template.Spec.Containers[0]
	if len(container.Args) != 2 {
		t.Errorf("expected 2 args (unchanged), got %d", len(container.Args))
	}
	if len(container.Ports) != 1 {
		t.Errorf("expected 1 port (unchanged), got %d", len(container.Ports))
	}
	if len(container.VolumeMounts) != 1 {
		t.Errorf("expected 1 volume mount (unchanged), got %d", len(container.VolumeMounts))
	}
	if len(got.Spec.Template.Spec.Volumes) != 1 {
		t.Errorf("expected 1 volume (unchanged), got %d", len(got.Spec.Template.Spec.Volumes))
	}
}

func TestEnsureWebhookEnabled_ArgPresentButPortMissing(t *testing.T) {
	s := newActionsTestScheme(t)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-operator", Namespace: "test-ns"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "manager",
						Args: []string{"--leader-elect", webhookArgEnabled},
					}},
				},
			},
		},
	}

	cli := fake.NewClientBuilder().WithScheme(s).WithObjects(dep).Build()
	err := ensureWebhookEnabled(context.Background(), cli, "test-operator", "test-ns", "test-operator-webhook-cert")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All 4 components are checked individually — missing port should be added.
	got := &appsv1.Deployment{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: "test-operator", Namespace: "test-ns"}, got); err != nil {
		t.Fatalf("failed to get deployment: %v", err)
	}

	container := got.Spec.Template.Spec.Containers[0]
	if len(container.Ports) != 1 {
		t.Errorf("expected 1 port added for partial drift repair, got %d", len(container.Ports))
	}
}

func TestEnsureWebhookEnabled_NothingPresent(t *testing.T) {
	s := newActionsTestScheme(t)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-operator", Namespace: "test-ns"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name: "manager",
						Args: []string{"--leader-elect"},
					}},
				},
			},
		},
	}

	cli := fake.NewClientBuilder().WithScheme(s).WithObjects(dep).Build()
	err := ensureWebhookEnabled(context.Background(), cli, "test-operator", "test-ns", "test-operator-webhook-cert")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got := &appsv1.Deployment{}
	if err := cli.Get(context.Background(), types.NamespacedName{Name: "test-operator", Namespace: "test-ns"}, got); err != nil {
		t.Fatalf("failed to get deployment: %v", err)
	}

	container := got.Spec.Template.Spec.Containers[0]

	// Verify webhook arg was added.
	foundArg := false
	for _, arg := range container.Args {
		if arg == webhookArgEnabled {
			foundArg = true
			break
		}
	}
	if !foundArg {
		t.Error("expected webhook arg to be added")
	}

	// Verify webhook port was added.
	foundPort := false
	for _, p := range container.Ports {
		if p.ContainerPort == webhookPort {
			foundPort = true
			break
		}
	}
	if !foundPort {
		t.Error("expected webhook port to be added")
	}

	// Verify volume mount was added.
	foundMount := false
	for _, vm := range container.VolumeMounts {
		if vm.Name == webhookVolumeName && vm.MountPath == webhookCertMountPath {
			foundMount = true
			break
		}
	}
	if !foundMount {
		t.Error("expected webhook volume mount to be added")
	}

	// Verify volume was added.
	foundVolume := false
	for _, v := range got.Spec.Template.Spec.Volumes {
		if v.Name == webhookVolumeName {
			foundVolume = true
			break
		}
	}
	if !foundVolume {
		t.Error("expected webhook volume to be added")
	}
}

func TestEnsureWebhookEnabled_NoContainers(t *testing.T) {
	s := newActionsTestScheme(t)

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "test-operator", Namespace: "test-ns"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{},
				},
			},
		},
	}

	cli := fake.NewClientBuilder().WithScheme(s).WithObjects(dep).Build()
	err := ensureWebhookEnabled(context.Background(), cli, "test-operator", "test-ns", "test-operator-webhook-cert")
	if err == nil {
		t.Fatal("expected error when deployment has no containers, got nil")
	}
}

// --- deployWebhookInfrastructure ---

func TestDeployWebhookInfrastructure_NoCertManagerCRD(t *testing.T) {
	s := newActionsTestScheme(t)
	m := newMonitoring(v1alpha1.MonitoringInstanceName)

	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource

	// The fake client does not reject unregistered unstructured GVKs, so we use
	// an interceptor to return a NoMatchError for the Issuer list call, which is
	// what a real cluster returns when the CRD is not installed.
	intercept := interceptor.Funcs{
		List: func(ctx context.Context, c client.WithWatch, list client.ObjectList, opts ...client.ListOption) error {
			if u, ok := list.(*unstructured.UnstructuredList); ok {
				if u.GetKind() == "IssuerList" {
					return &meta.NoResourceMatchError{PartialResource: schema.GroupVersionResource{
						Group:    gvk.CertManagerIssuer.Group,
						Version:  gvk.CertManagerIssuer.Version,
						Resource: "issuers",
					}}
				}
			}
			return c.List(ctx, list, opts...)
		},
	}

	cli := fake.NewClientBuilder().WithScheme(s).WithInterceptorFuncs(intercept).Build()
	err := deployWebhookInfrastructure(context.Background(), cli, m, cm, &sources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	webhookC := findCondition(m, conditions.ConditionWebhookAvailable)
	if webhookC == nil {
		t.Fatal("expected WebhookAvailable condition to be set")
	}
	if webhookC.Status != metav1.ConditionFalse {
		t.Errorf("expected WebhookAvailable=False, got %s", webhookC.Status)
	}
	if webhookC.Reason != "CertManagerNotAvailable" {
		t.Errorf("expected reason CertManagerNotAvailable, got %s", webhookC.Reason)
	}
}

func TestDeployWebhookInfrastructure_SecretNotFound(t *testing.T) {
	s := newActionsTestScheme(t)
	registerCRDs(s, gvk.CertManagerIssuer)

	t.Setenv("OPERATOR_NAME", "test-operator")
	t.Setenv("POD_NAMESPACE", "test-ns")

	m := newMonitoring(v1alpha1.MonitoringInstanceName)

	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource

	cli := fake.NewClientBuilder().WithScheme(s).Build()
	err := deployWebhookInfrastructure(context.Background(), cli, m, cm, &sources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	webhookC := findCondition(m, conditions.ConditionWebhookAvailable)
	if webhookC == nil {
		t.Fatal("expected WebhookAvailable condition to be set")
	}
	if webhookC.Status != metav1.ConditionFalse {
		t.Errorf("expected WebhookAvailable=False, got %s", webhookC.Status)
	}
	if webhookC.Reason != "TLSSecretPending" {
		t.Errorf("expected reason TLSSecretPending, got %s", webhookC.Reason)
	}
}

func TestDeployWebhookInfrastructure_TLSSecretEmpty(t *testing.T) {
	s := newActionsTestScheme(t)
	registerCRDs(s, gvk.CertManagerIssuer)

	t.Setenv("OPERATOR_NAME", "test-operator")
	t.Setenv("POD_NAMESPACE", "test-ns")

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "test-operator-webhook-cert", Namespace: "test-ns"},
		Data:       map[string][]byte{},
	}

	m := newMonitoring(v1alpha1.MonitoringInstanceName)

	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource

	cli := fake.NewClientBuilder().WithScheme(s).WithObjects(secret).Build()
	err := deployWebhookInfrastructure(context.Background(), cli, m, cm, &sources)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	webhookC := findCondition(m, conditions.ConditionWebhookAvailable)
	if webhookC == nil {
		t.Fatal("expected WebhookAvailable condition to be set")
	}
	if webhookC.Status != metav1.ConditionFalse {
		t.Errorf("expected WebhookAvailable=False, got %s", webhookC.Status)
	}
	if webhookC.Reason != "TLSSecretPending" {
		t.Errorf("expected reason TLSSecretPending, got %s", webhookC.Reason)
	}
}
