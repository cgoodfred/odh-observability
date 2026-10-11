package controller

import (
	"context"
	"testing"

	rendertemplate "github.com/opendatahub-io/odh-platform-utilities/pkg/render/template"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	v1alpha1 "github.com/opendatahub-io/odh-observability/api/v1alpha1"
	"github.com/opendatahub-io/odh-observability/internal/controller/conditions"
	"github.com/opendatahub-io/odh-observability/internal/controller/gvk"
)

func TestUsageLogsReceiverRendersTLS(t *testing.T) {
	ctx := context.Background()
	scheme := newActionsTestScheme(t)
	registerCRDs(scheme, gvk.OpenTelemetryCollector)
	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Spec.UsageLogs = &v1alpha1.UsageLogs{Storage: &v1alpha1.LokiStorageConfig{Type: "s3", SecretName: "storage"}}
	m.Status.UsageLogsEndpoint = "https://loki.example/otlp"
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(kubernetesAPIServerEndpointSlice()).Build()

	data, err := buildTemplateData(ctx, client, m, "")
	if err != nil {
		t.Fatal(err)
	}
	cm := conditions.NewConditionsManager(m, m.Generation)
	var sources []rendertemplate.TemplateSource
	if err := deployUsageLogsCollector(ctx, client, m, cm, &sources); err != nil {
		t.Fatal(err)
	}
	objects, err := rendertemplate.Render(ctx, scheme, sources, data)
	if err != nil {
		t.Fatal(err)
	}
	byKind := map[string]*unstructured.Unstructured{}
	for i := range objects {
		byKind[objects[i].GetKind()] = objects[i].DeepCopy()
	}
	collector := byKind["OpenTelemetryCollector"]
	service := byKind["Service"]
	if collector == nil || service == nil {
		t.Fatalf("expected collector and TLS Service; got %v", byKind)
	}
	if service.GetAnnotations()["service.beta.openshift.io/serving-cert-secret-name"] != usageLogsReceiverTLSSecretName {
		t.Fatal("TLS Service must request an OpenShift serving certificate")
	}
	selector, found, err := unstructured.NestedStringMap(service.Object, "spec", "selector")
	if err != nil || !found || selector["app.kubernetes.io/instance"] != data["Namespace"].(string)+".data-science-usage-logs" {
		t.Fatalf("TLS Service must select usage logs collector pods: selector=%v, error=%v", selector, err)
	}
	volumes, found, err := unstructured.NestedSlice(collector.Object, "spec", "volumes")
	if err != nil || !found {
		t.Fatalf("collector must mount serving certificate Secret: %v", err)
	}
	secretVolumes := map[string]string{}
	for _, value := range volumes {
		volume := value.(map[string]any)
		secret, ok := volume["secret"].(map[string]any)
		if ok {
			secretVolumes[volume["name"].(string)] = secret["secretName"].(string)
		}
	}
	if secretVolumes["usage-logs-otlp-tls"] != usageLogsReceiverTLSSecretName {
		t.Fatalf("collector must mount the serving certificate: %v", secretVolumes)
	}
	mounts, found, err := unstructured.NestedSlice(collector.Object, "spec", "volumeMounts")
	if err != nil || !found {
		t.Fatalf("collector must mount serving certificate files: %v", err)
	}
	certMountFound := false
	for _, value := range mounts {
		mount := value.(map[string]any)
		if mount["name"] == "usage-logs-otlp-tls" &&
			mount["mountPath"] == "/etc/otel-collector/otlp-tls" &&
			mount["readOnly"] == true {
			certMountFound = true
		}
	}
	if !certMountFound {
		t.Fatal("collector must mount the serving certificate read-only at its configured path")
	}
	for _, protocol := range []string{"grpc", "http"} {
		receiver, found, err := unstructured.NestedMap(collector.Object, "spec", "config", "receivers", "otlp", "protocols", protocol)
		if err != nil || !found {
			t.Fatalf("missing %s receiver: %v", protocol, err)
		}
		tls, ok := receiver["tls"].(map[string]any)
		if !ok ||
			tls["cert_file"] != "/etc/otel-collector/otlp-tls/tls.crt" ||
			tls["key_file"] != "/etc/otel-collector/otlp-tls/tls.key" {
			t.Errorf("%s receiver must load the mounted serving certificate", protocol)
		}
		if tls["reload_interval"] != "1h" {
			t.Errorf("%s receiver must reload renewed serving certificates", protocol)
		}
	}
}
