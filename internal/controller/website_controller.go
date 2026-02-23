/*
Copyright 2026.

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
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	webappv1 "github.com/kurigashvili/k8s-operator/api/v1"
)

// WebsiteReconciler reconciles a Website object
type WebsiteReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=webapp.example.com,resources=websites,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=webapp.example.com,resources=websites/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=webapp.example.com,resources=websites/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

func (r *WebsiteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	// 1. Fetch the Website CR
	var website webappv1.Website
	if err := r.Get(ctx, req.NamespacedName, &website); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Website resource not found; ignoring since it must have been deleted")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// 2. Reconcile Deployment
	if err := r.reconcileDeployment(ctx, &website); err != nil {
		return ctrl.Result{}, err
	}

	// 3. Reconcile Service
	if err := r.reconcileService(ctx, &website); err != nil {
		return ctrl.Result{}, err
	}

	// 4. Update status
	if err := r.updateStatus(ctx, &website); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *WebsiteReconciler) reconcileDeployment(ctx context.Context, website *webappv1.Website) error {
	log := logf.FromContext(ctx)
	desired := r.desiredDeployment(website)

	// Set owner reference so the Deployment gets garbage-collected with the CR
	if err := ctrl.SetControllerReference(website, desired, r.Scheme); err != nil {
		return err
	}

	var existing appsv1.Deployment
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), &existing)
	if apierrors.IsNotFound(err) {
		log.Info("Creating Deployment", "name", desired.Name)
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// Update if the spec has drifted
	if !equality.Semantic.DeepEqual(existing.Spec, desired.Spec) {
		log.Info("Updating Deployment", "name", desired.Name)
		existing.Spec = desired.Spec
		return r.Update(ctx, &existing)
	}

	return nil
}

func (r *WebsiteReconciler) reconcileService(ctx context.Context, website *webappv1.Website) error {
	log := logf.FromContext(ctx)
	desired := r.desiredService(website)

	if err := ctrl.SetControllerReference(website, desired, r.Scheme); err != nil {
		return err
	}

	var existing corev1.Service
	err := r.Get(ctx, client.ObjectKeyFromObject(desired), &existing)
	if apierrors.IsNotFound(err) {
		log.Info("Creating Service", "name", desired.Name)
		return r.Create(ctx, desired)
	}
	if err != nil {
		return err
	}

	// Update mutable fields if drifted
	if !equality.Semantic.DeepEqual(existing.Spec.Ports, desired.Spec.Ports) ||
		!equality.Semantic.DeepEqual(existing.Spec.Selector, desired.Spec.Selector) {
		log.Info("Updating Service", "name", desired.Name)
		existing.Spec.Ports = desired.Spec.Ports
		existing.Spec.Selector = desired.Spec.Selector
		return r.Update(ctx, &existing)
	}

	return nil
}

func (r *WebsiteReconciler) updateStatus(ctx context.Context, website *webappv1.Website) error {
	var dep appsv1.Deployment
	depKey := client.ObjectKey{Namespace: website.Namespace, Name: website.Name}
	if err := r.Get(ctx, depKey, &dep); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		return err
	}

	website.Status.AvailableReplicas = dep.Status.AvailableReplicas

	// Set the Available condition
	condition := metav1.Condition{
		Type:               "Available",
		ObservedGeneration: website.Generation,
	}
	if dep.Status.AvailableReplicas > 0 {
		condition.Status = metav1.ConditionTrue
		condition.Reason = "DeploymentAvailable"
		condition.Message = fmt.Sprintf("%d replica(s) available", dep.Status.AvailableReplicas)
	} else {
		condition.Status = metav1.ConditionFalse
		condition.Reason = "DeploymentUnavailable"
		condition.Message = "No replicas available yet"
	}
	setCondition(&website.Status.Conditions, condition)

	return r.Status().Update(ctx, website)
}

// labels returns the standard set of labels for child resources.
func labels(website *webappv1.Website) map[string]string {
	return map[string]string{
		"app.kubernetes.io/name":       "website",
		"app.kubernetes.io/instance":   website.Name,
		"app.kubernetes.io/managed-by": "website-controller",
	}
}

func (r *WebsiteReconciler) desiredDeployment(website *webappv1.Website) *appsv1.Deployment {
	lbls := labels(website)

	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      website.Name,
			Namespace: website.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: website.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: lbls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: lbls,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "website",
							Image: website.Spec.Image,
							Ports: []corev1.ContainerPort{
								{
									ContainerPort: website.Spec.Port,
									Protocol:      corev1.ProtocolTCP,
								},
							},
						},
					},
				},
			},
		},
	}
}

func (r *WebsiteReconciler) desiredService(website *webappv1.Website) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      website.Name,
			Namespace: website.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: labels(website),
			Ports: []corev1.ServicePort{
				{
					Port:       website.Spec.Port,
					TargetPort: intstr.FromInt32(website.Spec.Port),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}
}

// setCondition adds or updates a condition in the slice.
func setCondition(conditions *[]metav1.Condition, newCondition metav1.Condition) {
	now := metav1.Now()
	newCondition.LastTransitionTime = now

	for i, c := range *conditions {
		if c.Type == newCondition.Type {
			if c.Status != newCondition.Status {
				newCondition.LastTransitionTime = now
			} else {
				newCondition.LastTransitionTime = c.LastTransitionTime
			}
			(*conditions)[i] = newCondition
			return
		}
	}
	*conditions = append(*conditions, newCondition)
}

// SetupWithManager sets up the controller with the Manager.
func (r *WebsiteReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&webappv1.Website{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Named("website").
		Complete(r)
}
