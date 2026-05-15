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

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	platformcommon "github.com/opendatahub-io/odh-platform-utilities/api/common"
	"github.com/opendatahub-io/odh-platform-utilities/pkg/deploy"
	routev1 "github.com/openshift/api/route/v1"

	v1alpha1 "github.com/opendatahub-io/odh-observability/api/v1alpha1"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(v1alpha1.AddToScheme(s))
	utilruntime.Must(routev1.Install(s))
	utilruntime.Must(extv1.AddToScheme(s))
	utilruntime.Must(appsv1.AddToScheme(s))
	utilruntime.Must(batchv1.AddToScheme(s))
	utilruntime.Must(networkingv1.AddToScheme(s))
	utilruntime.Must(rbacv1.AddToScheme(s))
	return s
}

func newTestReconciler(t *testing.T, s *runtime.Scheme, c client.Client) *MonitoringReconciler {
	t.Helper()
	cs := fakeclientset.NewSimpleClientset()
	return &MonitoringReconciler{
		Client:          c,
		Scheme:          s,
		Deployer:        deploy.NewDeployer(deploy.WithFieldOwner("monitoring")),
		DynamicClient:   fakedynamic.NewSimpleDynamicClient(s),
		DiscoveryClient: cs.Discovery(),
	}
}

func newMonitoring(name string) *v1alpha1.Monitoring {
	return &v1alpha1.Monitoring{
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Generation: 1,
		},
		Spec: v1alpha1.MonitoringSpec{
			ManagementSpec: platformcommon.ManagementSpec{
				ManagementState: platformcommon.Managed,
			},
			Namespace: "test-ns",
		},
	}
}

// TestReconcile_Removed: Monitoring with Removed state should short-circuit, set
// Ready=False and ProvisioningSucceeded=False, and not return an error.
func TestReconcile_Removed(t *testing.T) {
	s := newTestScheme(t)
	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Spec.ManagementState = platformcommon.Removed

	r := newTestReconciler(t, s, fake.NewClientBuilder().WithScheme(s).WithObjects(m).WithStatusSubresource(m).Build())

	_, err := r.reconcile(context.Background(), m)
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var ready, provisioning string
	for _, c := range m.Status.Status.Conditions {
		switch c.Type {
		case string(platformcommon.ConditionTypeReady):
			ready = string(c.Status)
		case string(platformcommon.ConditionTypeProvisioningSucceeded):
			provisioning = string(c.Status)
		}
	}
	if ready != string(metav1.ConditionFalse) {
		t.Errorf("Ready: want False, got %q", ready)
	}
	if provisioning != string(metav1.ConditionFalse) {
		t.Errorf("ProvisioningSucceeded: want False, got %q", provisioning)
	}
}

// TestReconcile_PreconditionsFailed: no operators installed, nothing configured.
// Should set MonitoringAvailable=False, Ready=False, no error returned.
func TestReconcile_PreconditionsFailed(t *testing.T) {
	s := newTestScheme(t)

	// Register OperatorCondition GVK in the fake client tracker.
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "operators.coreos.com", Version: "v2", Kind: "OperatorCondition",
	}, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "operators.coreos.com", Version: "v2", Kind: "OperatorConditionList",
	}, &unstructured.UnstructuredList{})

	// Monitoring CR requesting metrics (so preconditions are checked).
	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Spec.Metrics = &v1alpha1.Metrics{}

	r := newTestReconciler(t, s, fake.NewClientBuilder().WithScheme(s).WithObjects(m).WithStatusSubresource(m).Build())

	_, err := r.reconcile(context.Background(), m)
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var ready, monAvail string
	for _, c := range m.Status.Status.Conditions {
		switch c.Type {
		case string(platformcommon.ConditionTypeReady):
			ready = string(c.Status)
		case "MonitoringAvailable":
			monAvail = string(c.Status)
		}
	}
	if ready != string(metav1.ConditionFalse) {
		t.Errorf("Ready: want False, got %q", ready)
	}
	if monAvail != string(metav1.ConditionFalse) {
		t.Errorf("MonitoringAvailable: want False, got %q", monAvail)
	}
}

// TestReconcile_NothingConfigured: all operators present, nothing configured.
// Should be Ready=True. Degraded is True because the webhook infrastructure
// always deploys and WebhookAvailable is False when cert-manager is not
// installed (the fake client has no cert-manager CRDs registered).
func TestReconcile_NothingConfigured(t *testing.T) {
	s := newTestScheme(t)

	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "operators.coreos.com", Version: "v2", Kind: "OperatorCondition",
	}, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "operators.coreos.com", Version: "v2", Kind: "OperatorConditionList",
	}, &unstructured.UnstructuredList{})

	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	// No Metrics/Traces configured: no operator precondition checks triggered.

	r := newTestReconciler(t, s, fake.NewClientBuilder().WithScheme(s).WithObjects(m).WithStatusSubresource(m).Build())

	_, err := r.reconcile(context.Background(), m)
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var ready, degraded string
	for _, c := range m.Status.Status.Conditions {
		switch c.Type {
		case string(platformcommon.ConditionTypeReady):
			ready = string(c.Status)
		case string(platformcommon.ConditionTypeDegraded):
			degraded = string(c.Status)
		}
	}
	if ready != string(metav1.ConditionTrue) {
		t.Errorf("Ready: want True, got %q", ready)
	}
	// Degraded is True because WebhookAvailable is False when cert-manager is not installed.
	if degraded != string(metav1.ConditionTrue) {
		t.Errorf("Degraded: want True (webhook unavailable without cert-manager), got %q", degraded)
	}
}

// TestSyncStatusURL_RoutePresent: when metrics are configured and the route exists
// with a host, status.url is populated.
func TestSyncStatusURL_RoutePresent(t *testing.T) {
	s := newTestScheme(t)
	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Spec.Metrics = &v1alpha1.Metrics{}

	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      thanosQuerierRouteName,
			Namespace: m.Spec.Namespace,
		},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{
				{Host: "thanos.example.com"},
			},
		},
	}

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(m, route).WithStatusSubresource(route).Build()
	syncStatusURL(context.Background(), c, m)

	want := "https://thanos.example.com"
	if m.Status.URL != want {
		t.Errorf("Status.URL: want %q, got %q", want, m.Status.URL)
	}
}

// TestSyncStatusURL_RouteMissing: when the route doesn't exist yet, URL is empty.
func TestSyncStatusURL_RouteMissing(t *testing.T) {
	s := newTestScheme(t)
	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Spec.Metrics = &v1alpha1.Metrics{}
	m.Status.URL = "https://stale.example.com"

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(m).Build()
	syncStatusURL(context.Background(), c, m)

	if m.Status.URL != "" {
		t.Errorf("Status.URL: want empty, got %q", m.Status.URL)
	}
}

// TestSyncStatusURL_NoMetrics: when metrics are not configured, URL is cleared.
func TestSyncStatusURL_NoMetrics(t *testing.T) {
	s := newTestScheme(t)
	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Status.URL = "https://stale.example.com"

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(m).Build()
	syncStatusURL(context.Background(), c, m)

	if m.Status.URL != "" {
		t.Errorf("Status.URL: want empty, got %q", m.Status.URL)
	}
}

// TestReconcile_ReleasesPopulated: reconcile should populate releases with operator identity.
func TestReconcile_ReleasesPopulated(t *testing.T) {
	s := newTestScheme(t)

	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "operators.coreos.com", Version: "v2", Kind: "OperatorCondition",
	}, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "operators.coreos.com", Version: "v2", Kind: "OperatorConditionList",
	}, &unstructured.UnstructuredList{})

	m := newMonitoring(v1alpha1.MonitoringInstanceName)

	r := newTestReconciler(t, s, fake.NewClientBuilder().WithScheme(s).WithObjects(m).WithStatusSubresource(m).Build())

	_, err := r.reconcile(context.Background(), m)
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	releases := m.GetReleaseStatus().Releases
	if len(releases) == 0 {
		t.Fatal("releases not populated")
	}
	if releases[0].Name != v1alpha1.MonitoringServiceName {
		t.Errorf("releases[0].Name: want %q, got %q", v1alpha1.MonitoringServiceName, releases[0].Name)
	}
	if releases[0].RepoURL == "" {
		t.Error("releases[0].RepoURL: want non-empty")
	}
}

// TestReconcile_ObservedGenerationSet: ObservedGeneration must match the CR generation.
func TestReconcile_ObservedGenerationSet(t *testing.T) {
	s := newTestScheme(t)

	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "operators.coreos.com", Version: "v2", Kind: "OperatorCondition",
	}, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "operators.coreos.com", Version: "v2", Kind: "OperatorConditionList",
	}, &unstructured.UnstructuredList{})

	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Generation = 7

	r := newTestReconciler(t, s, fake.NewClientBuilder().WithScheme(s).WithObjects(m).WithStatusSubresource(m).Build())

	_, err := r.reconcile(context.Background(), m)
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	if got := m.Status.Status.ObservedGeneration; got != 7 {
		t.Errorf("ObservedGeneration: want 7, got %d", got)
	}
}

// TestReconcile_Finalizer_AddedOnCreate: the public Reconcile method should add the
// monitoring finalizer to a new Monitoring CR that does not already have it.
func TestReconcile_Finalizer_AddedOnCreate(t *testing.T) {
	s := newTestScheme(t)

	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "operators.coreos.com", Version: "v2", Kind: "OperatorCondition",
	}, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "operators.coreos.com", Version: "v2", Kind: "OperatorConditionList",
	}, &unstructured.UnstructuredList{})

	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	// Ensure no finalizers are set initially.
	m.Finalizers = nil

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(m).WithStatusSubresource(m).Build()
	r := newTestReconciler(t, s, c)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: v1alpha1.MonitoringInstanceName}}
	_, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// Re-fetch the CR from the fake client to see the updated finalizers.
	updated := &v1alpha1.Monitoring{}
	if err := c.Get(context.Background(), req.NamespacedName, updated); err != nil {
		t.Fatalf("failed to get updated Monitoring: %v", err)
	}

	if !controllerutil.ContainsFinalizer(updated, monitoringFinalizer) {
		t.Errorf("expected finalizer %q to be present, got finalizers: %v", monitoringFinalizer, updated.Finalizers)
	}
}

// TestReconcile_Finalizer_CleanupOnDelete: when a Monitoring CR has a non-zero
// DeletionTimestamp and the finalizer is present, Reconcile runs deleteAllOwned
// to clean up resources before removing the finalizer.
//
// With fake clients the GC library's SelfSubjectRulesReview call fails, so
// deleteAllOwned returns an error. The controller correctly propagates that
// error and retains the finalizer (the next reconcile will retry). This test
// verifies that retry-safe behavior: the finalizer must NOT be removed when
// cleanup fails.
func TestReconcile_Finalizer_CleanupOnDelete(t *testing.T) {
	s := newTestScheme(t)

	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "operators.coreos.com", Version: "v2", Kind: "OperatorCondition",
	}, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "operators.coreos.com", Version: "v2", Kind: "OperatorConditionList",
	}, &unstructured.UnstructuredList{})

	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	now := metav1.Now()
	m.DeletionTimestamp = &now
	// The monitoring finalizer plus a dummy finalizer. The dummy prevents the
	// fake client from auto-deleting the object when all finalizers are removed.
	m.Finalizers = []string{monitoringFinalizer, "test/dummy"}

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(m).WithStatusSubresource(m).Build()
	r := newTestReconciler(t, s, c)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: v1alpha1.MonitoringInstanceName}}
	_, err := r.Reconcile(context.Background(), req)
	// deleteAllOwned fails with the fake client (SelfSubjectRulesReview is not
	// supported), so Reconcile must return an error to trigger a retry.
	if err == nil {
		t.Fatal("expected Reconcile to return an error when deleteAllOwned fails, got nil")
	}

	// The finalizer must still be present because cleanup did not succeed.
	updated := &v1alpha1.Monitoring{}
	if err := c.Get(context.Background(), req.NamespacedName, updated); err != nil {
		t.Fatalf("failed to get updated Monitoring: %v", err)
	}

	if !controllerutil.ContainsFinalizer(updated, monitoringFinalizer) {
		t.Errorf("expected finalizer %q to remain (cleanup failed), got finalizers: %v", monitoringFinalizer, updated.Finalizers)
	}
	if !controllerutil.ContainsFinalizer(updated, "test/dummy") {
		t.Errorf("expected dummy finalizer to remain, got finalizers: %v", updated.Finalizers)
	}
}

// TestReconcile_Finalizer_AlreadyPresent: when the finalizer is already on the CR,
// Reconcile should skip the add step and proceed without error.
func TestReconcile_Finalizer_AlreadyPresent(t *testing.T) {
	s := newTestScheme(t)

	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "operators.coreos.com", Version: "v2", Kind: "OperatorCondition",
	}, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "operators.coreos.com", Version: "v2", Kind: "OperatorConditionList",
	}, &unstructured.UnstructuredList{})

	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Finalizers = []string{monitoringFinalizer}

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(m).WithStatusSubresource(m).Build()
	r := newTestReconciler(t, s, c)

	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: v1alpha1.MonitoringInstanceName}}
	_, err := r.Reconcile(context.Background(), req)
	if err != nil {
		t.Fatalf("Reconcile returned error: %v", err)
	}

	// Re-fetch and verify the finalizer is still there (not duplicated).
	updated := &v1alpha1.Monitoring{}
	if err := c.Get(context.Background(), req.NamespacedName, updated); err != nil {
		t.Fatalf("failed to get updated Monitoring: %v", err)
	}

	if !controllerutil.ContainsFinalizer(updated, monitoringFinalizer) {
		t.Errorf("expected finalizer %q to remain present, got finalizers: %v", monitoringFinalizer, updated.Finalizers)
	}
	// Ensure no duplicate finalizers.
	count := 0
	for _, f := range updated.Finalizers {
		if f == monitoringFinalizer {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 instance of finalizer %q, got %d (finalizers: %v)", monitoringFinalizer, count, updated.Finalizers)
	}
}

// TestReconcile_Removed_EmptyNamespace: when ManagementState is Removed and
// Spec.Namespace is empty, reconcile should still succeed (Ready=False with
// Removed reason). The deleteAllOwned error from empty namespace is logged but
// does not block the reconcile.
func TestReconcile_Removed_EmptyNamespace(t *testing.T) {
	s := newTestScheme(t)

	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "operators.coreos.com", Version: "v2", Kind: "OperatorCondition",
	}, &unstructured.Unstructured{})
	s.AddKnownTypeWithName(schema.GroupVersionKind{
		Group: "operators.coreos.com", Version: "v2", Kind: "OperatorConditionList",
	}, &unstructured.UnstructuredList{})

	m := newMonitoring(v1alpha1.MonitoringInstanceName)
	m.Spec.ManagementState = platformcommon.Removed
	m.Spec.Namespace = ""

	c := fake.NewClientBuilder().WithScheme(s).WithObjects(m).WithStatusSubresource(m).Build()
	r := newTestReconciler(t, s, c)

	_, err := r.reconcile(context.Background(), m)
	if err != nil {
		t.Fatalf("reconcile returned error: %v", err)
	}

	var ready, readyReason string
	for _, cond := range m.Status.Status.Conditions {
		if cond.Type == string(platformcommon.ConditionTypeReady) {
			ready = string(cond.Status)
			readyReason = cond.Reason
		}
	}
	if ready != string(metav1.ConditionFalse) {
		t.Errorf("Ready: want False, got %q", ready)
	}
	if readyReason != "Removed" {
		t.Errorf("Ready reason: want %q, got %q", "Removed", readyReason)
	}
}
