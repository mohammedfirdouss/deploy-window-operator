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
	"slices"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	policyv1alpha1 "github.com/mohammedfirdouss/deploy-window-operator/api/v1alpha1"
	"github.com/mohammedfirdouss/deploy-window-operator/internal/schedule"
)

const (
	// PausedByAnnotation marks a Deployment this controller paused, with the
	// name of the DeploymentWindow responsible. Deployments paused by anyone
	// else (no annotation) are never resumed by this controller.
	PausedByAnnotation = "deploywindow.dev/paused-by"
	finalizerName      = "deploywindow.dev/resume-on-delete"
	conditionReady     = "Ready"
)

// DeploymentWindowReconciler reconciles a DeploymentWindow object.
type DeploymentWindowReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// Now returns the current time. Tests replace it; nil means time.Now.
	Now func() time.Time
}

func (r *DeploymentWindowReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// +kubebuilder:rbac:groups=policy.deploywindow.dev,resources=deploymentwindows,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=policy.deploywindow.dev,resources=deploymentwindows/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=policy.deploywindow.dev,resources=deploymentwindows/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update;patch

// Reconcile pauses the selected Deployments while the window is closed and
// resumes the ones it paused once the window opens, is suspended, or is deleted.
func (r *DeploymentWindowReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var dw policyv1alpha1.DeploymentWindow
	if err := r.Get(ctx, req.NamespacedName, &dw); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Being deleted: resume everything this window paused, then let it go.
	if !dw.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(&dw, finalizerName) {
			if err := r.resumeAll(ctx, &dw); err != nil {
				return ctrl.Result{}, err
			}
			controllerutil.RemoveFinalizer(&dw, finalizerName)
			if err := r.Update(ctx, &dw); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}
	if controllerutil.AddFinalizer(&dw, finalizerName) {
		if err := r.Update(ctx, &dw); err != nil {
			return ctrl.Result{}, err
		}
	}

	now := r.now()
	open, next, err := schedule.Evaluate(now, dw.Spec.TimeZone, toScheduleWindows(dw.Spec.Windows))
	if err != nil {
		// A bad spec should not leave Deployments paused forever.
		if rerr := r.resumeAll(ctx, &dw); rerr != nil {
			return ctrl.Result{}, rerr
		}
		return ctrl.Result{}, r.setStatus(ctx, &dw, func(s *policyv1alpha1.DeploymentWindowStatus) {
			s.Open = false
			s.NextTransition = nil
			s.PausedDeployments = nil
			meta.SetStatusCondition(&s.Conditions, metav1.Condition{
				Type: conditionReady, Status: metav1.ConditionFalse,
				Reason: "InvalidSpec", Message: err.Error(), ObservedGeneration: dw.Generation,
			})
		})
	}

	selector, err := metav1.LabelSelectorAsSelector(&dw.Spec.Selector)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("parse selector: %w", err)
	}
	var deps appsv1.DeploymentList
	if err := r.List(ctx, &deps, client.InNamespace(dw.Namespace)); err != nil {
		return ctrl.Result{}, err
	}

	shouldPause := !open && !dw.Spec.Suspend
	var matched int32
	var pausedByUs []string
	for i := range deps.Items {
		d := &deps.Items[i]
		ours := d.Annotations[PausedByAnnotation] == dw.Name
		isMatch := selector.Matches(labels.Set(d.Labels))
		if isMatch {
			matched++
		}
		switch {
		case isMatch && shouldPause && !d.Spec.Paused:
			if err := r.setPaused(ctx, d, true, dw.Name); err != nil {
				return ctrl.Result{}, err
			}
			log.Info("paused deployment outside window", "deployment", d.Name)
			pausedByUs = append(pausedByUs, d.Name)
		case isMatch && shouldPause && ours:
			pausedByUs = append(pausedByUs, d.Name)
		case ours && (!shouldPause || !isMatch):
			// Window opened, was suspended, or the Deployment no longer matches.
			if err := r.setPaused(ctx, d, false, ""); err != nil {
				return ctrl.Result{}, err
			}
			log.Info("resumed deployment", "deployment", d.Name)
		}
	}
	slices.Sort(pausedByUs)

	msg := fmt.Sprintf("window closed; %d deployment(s) paused", len(pausedByUs))
	switch {
	case dw.Spec.Suspend:
		msg = "suspended; no deployments paused"
	case open:
		msg = "window open; rollouts allowed"
	}
	if err := r.setStatus(ctx, &dw, func(s *policyv1alpha1.DeploymentWindowStatus) {
		s.Open = open
		s.NextTransition = &metav1.Time{Time: next}
		s.MatchedDeployments = matched
		s.PausedDeployments = pausedByUs
		meta.SetStatusCondition(&s.Conditions, metav1.Condition{
			Type: conditionReady, Status: metav1.ConditionTrue,
			Reason: "Applied", Message: msg, ObservedGeneration: dw.Generation,
		})
	}); err != nil {
		return ctrl.Result{}, err
	}

	// Wake up exactly at the next boundary (plus a second of slack).
	return ctrl.Result{RequeueAfter: next.Sub(now) + time.Second}, nil
}

func (r *DeploymentWindowReconciler) setPaused(ctx context.Context, d *appsv1.Deployment, paused bool, owner string) error {
	patch := client.MergeFrom(d.DeepCopy())
	d.Spec.Paused = paused
	if paused {
		if d.Annotations == nil {
			d.Annotations = map[string]string{}
		}
		d.Annotations[PausedByAnnotation] = owner
	} else {
		delete(d.Annotations, PausedByAnnotation)
	}
	return r.Patch(ctx, d, patch)
}

// resumeAll resumes every Deployment in the namespace that this window paused.
func (r *DeploymentWindowReconciler) resumeAll(ctx context.Context, dw *policyv1alpha1.DeploymentWindow) error {
	var deps appsv1.DeploymentList
	if err := r.List(ctx, &deps, client.InNamespace(dw.Namespace)); err != nil {
		return err
	}
	for i := range deps.Items {
		d := &deps.Items[i]
		if d.Annotations[PausedByAnnotation] == dw.Name {
			if err := r.setPaused(ctx, d, false, ""); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *DeploymentWindowReconciler) setStatus(ctx context.Context, dw *policyv1alpha1.DeploymentWindow, mutate func(*policyv1alpha1.DeploymentWindowStatus)) error {
	// Update (not a merge patch) so false and empty values are written too;
	// a merge patch would drop "open: false" when nothing else changed.
	mutate(&dw.Status)
	dw.Status.ObservedGeneration = dw.Generation
	return r.Status().Update(ctx, dw)
}

func toScheduleWindows(in []policyv1alpha1.Window) []schedule.Window {
	out := make([]schedule.Window, len(in))
	for i, w := range in {
		out[i] = schedule.Window{Days: w.Days, Start: w.Start, End: w.End}
	}
	return out
}

// windowsForDeployment re-queues every DeploymentWindow in a Deployment's
// namespace when that Deployment changes, so a new or relabelled Deployment
// is paused (or released) without waiting for the next window boundary.
func (r *DeploymentWindowReconciler) windowsForDeployment(ctx context.Context, obj client.Object) []reconcile.Request {
	var list policyv1alpha1.DeploymentWindowList
	if err := r.List(ctx, &list, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}
	reqs := make([]reconcile.Request, 0, len(list.Items))
	for _, dw := range list.Items {
		reqs = append(reqs, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: dw.Namespace, Name: dw.Name}})
	}
	return reqs
}

// SetupWithManager sets up the controller with the Manager.
func (r *DeploymentWindowReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&policyv1alpha1.DeploymentWindow{}).
		Watches(&appsv1.Deployment{}, handler.EnqueueRequestsFromMapFunc(r.windowsForDeployment)).
		Named("deploymentwindow").
		Complete(r)
}
