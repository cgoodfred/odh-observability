package controller

import (
	"context"
	"fmt"

	platformcommon "github.com/opendatahub-io/odh-platform-utilities/api/common"
	"github.com/opendatahub-io/operator-rbac-toolkit/pkg/crossns"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v1alpha1 "github.com/opendatahub-io/odh-observability/api/v1alpha1"
)

const (
	TargetAllocatorRBACScoperOwnerLabelKey   = "app.kubernetes.io/name"
	TargetAllocatorRBACScoperOwnerLabelValue = "odh-observability-targetallocator-rbac"
	rbacVerbGet                              = "get"
	rbacVerbList                             = "list"
	rbacVerbWatch                            = "watch"
)

// TargetAllocatorRBACScoper moves the TargetAllocator Secret RoleBindings into
// a separate ServiceAccount trust domain. The manager keeps its operational
// permissions but does not create these cross-namespace RBAC objects.
type TargetAllocatorRBACScoper struct {
	client.Client

	Reconciler *crossns.Reconciler
}

func (r *TargetAllocatorRBACScoper) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	monitoring := &v1alpha1.Monitoring{}
	if err := r.Get(ctx, req.NamespacedName, monitoring); err != nil {
		if k8serr.IsNotFound(err) {
			return ctrl.Result{}, r.Reconciler.Teardown(ctx)
		}
		return ctrl.Result{}, err
	}

	if !monitoring.DeletionTimestamp.IsZero() ||
		monitoring.Spec.ManagementState == platformcommon.Removed ||
		monitoring.Spec.Metrics == nil {
		return ctrl.Result{}, r.Reconciler.Teardown(ctx)
	}

	namespaces, err := listAllNamespaces(ctx, r.Client, monitoringNamespace(monitoring))
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("listing TargetAllocator Secret namespaces: %w", err)
	}

	if err := r.removeLegacyTargetAllocatorBindings(ctx); err != nil {
		return ctrl.Result{}, err
	}

	ruleSets := make([]crossns.RuleSet, 0, len(namespaces))
	for _, namespace := range namespaces {
		ruleSets = append(ruleSets, crossns.RuleSet{
			RoleName:  targetAllocatorSecretRBACName,
			Namespace: namespace,
			Rules: []rbacv1.PolicyRule{{
				APIGroups: []string{""},
				Resources: []string{"secrets"},
				Verbs:     []string{rbacVerbGet, rbacVerbList, rbacVerbWatch},
			}},
		})
	}

	if err := r.Reconciler.Apply(ctx, crossns.SubjectRef{
		Name:      targetAllocatorServiceAccountName,
		Namespace: monitoringNamespace(monitoring),
	}, ruleSets); err != nil {
		return ctrl.Result{}, fmt.Errorf("applying TargetAllocator RBAC: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *TargetAllocatorRBACScoper) removeLegacyTargetAllocatorBindings(ctx context.Context) error {
	bindings := &rbacv1.RoleBindingList{}
	if err := r.List(ctx, bindings); err != nil {
		return fmt.Errorf("listing legacy TargetAllocator RoleBindings: %w", err)
	}

	for i := range bindings.Items {
		binding := bindings.Items[i]
		if binding.Name != targetAllocatorSecretRBACName {
			continue
		}
		if err := r.Delete(ctx, &binding); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("deleting legacy TargetAllocator RoleBinding %s/%s: %w", binding.Namespace, binding.Name, err)
		}
	}
	return nil
}

func (r *MonitoringReconciler) cleanupTargetAllocatorScoperRBAC(ctx context.Context) error {
	labels := client.MatchingLabels{
		crossns.ManagedLabelKey:                crossns.ManagedLabelValue,
		TargetAllocatorRBACScoperOwnerLabelKey: TargetAllocatorRBACScoperOwnerLabelValue,
	}

	roles := &rbacv1.RoleList{}
	if err := r.List(ctx, roles, labels); err != nil {
		return fmt.Errorf("listing toolkit TargetAllocator Roles: %w", err)
	}
	for i := range roles.Items {
		role := roles.Items[i]
		if role.Name != targetAllocatorSecretRBACName {
			continue
		}
		if err := r.Delete(ctx, &role); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("deleting toolkit TargetAllocator Role %s/%s: %w", role.Namespace, role.Name, err)
		}
	}

	bindings := &rbacv1.RoleBindingList{}
	if err := r.List(ctx, bindings, labels); err != nil {
		return fmt.Errorf("listing toolkit TargetAllocator RoleBindings: %w", err)
	}
	for i := range bindings.Items {
		binding := bindings.Items[i]
		if binding.Name != targetAllocatorSecretRBACName+"-binding" {
			continue
		}
		if err := r.Delete(ctx, &binding); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("deleting toolkit TargetAllocator RoleBinding %s/%s: %w", binding.Namespace, binding.Name, err)
		}
	}
	return nil
}

func (r *TargetAllocatorRBACScoper) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.Monitoring{}).
		Watches(&corev1.Namespace{}, handler.EnqueueRequestsFromMapFunc(r.monitoringRequests)).
		Complete(r)
}

func (r *TargetAllocatorRBACScoper) monitoringRequests(ctx context.Context, _ client.Object) []reconcile.Request {
	monitoringList := &v1alpha1.MonitoringList{}
	if err := r.List(ctx, monitoringList); err != nil {
		return nil
	}
	requests := make([]reconcile.Request, 0, len(monitoringList.Items))
	for i := range monitoringList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&monitoringList.Items[i]),
		})
	}
	return requests
}

var _ reconcile.Reconciler = (*TargetAllocatorRBACScoper)(nil)
