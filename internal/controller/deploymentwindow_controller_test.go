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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	policyv1alpha1 "github.com/mohammedfirdouss/deploy-window-operator/api/v1alpha1"
)

// Monday 16 Nov 2026. The test window is Mon-Fri 09:00-17:00 UTC.
var (
	insideWindow  = time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	outsideWindow = time.Date(2026, 11, 16, 20, 0, 0, 0, time.UTC)
)

const (
	productionTier = "prod"
	tierLabel      = "tier"
)

var _ = Describe("DeploymentWindow controller", func() {
	ctx := context.Background()
	var ns string
	var counter int

	newDeployment := func(name string, lbls map[string]string, paused bool) *appsv1.Deployment {
		d := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: lbls},
			Spec: appsv1.DeploymentSpec{
				Paused:   paused,
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
					Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "nginx"}}},
				},
			},
		}
		Expect(k8sClient.Create(ctx, d)).To(Succeed())
		return d
	}

	newWindow := func(name, tz string, suspend bool) *policyv1alpha1.DeploymentWindow {
		dw := &policyv1alpha1.DeploymentWindow{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec: policyv1alpha1.DeploymentWindowSpec{
				Selector: metav1.LabelSelector{MatchLabels: map[string]string{tierLabel: productionTier}},
				TimeZone: tz,
				Windows: []policyv1alpha1.Window{{
					Days: []string{"Mon", "Tue", "Wed", "Thu", "Fri"}, Start: "09:00", End: "17:00",
				}},
				Suspend: suspend,
			},
		}
		Expect(k8sClient.Create(ctx, dw)).To(Succeed())
		return dw
	}

	reconcileAt := func(now time.Time, name string) reconcile.Result {
		r := &DeploymentWindowReconciler{Client: k8sClient, Scheme: k8sClient.Scheme(), Now: func() time.Time { return now }}
		res, err := r.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Namespace: ns, Name: name}})
		Expect(err).NotTo(HaveOccurred())
		return res
	}

	getDeployment := func(name string) *appsv1.Deployment {
		d := &appsv1.Deployment{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, d)).To(Succeed())
		return d
	}
	getWindow := func(name string) *policyv1alpha1.DeploymentWindow {
		dw := &policyv1alpha1.DeploymentWindow{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: name}, dw)).To(Succeed())
		return dw
	}

	BeforeEach(func() {
		counter++
		ns = fmt.Sprintf("dw-test-%d", counter)
		Expect(k8sClient.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: ns}})).To(Succeed())
	})

	It("pauses matching Deployments outside the window and leaves others alone", func() {
		newDeployment("api", map[string]string{tierLabel: productionTier}, false)
		newDeployment("worker", map[string]string{tierLabel: "dev"}, false)
		newWindow("business-hours", "UTC", false)

		res := reconcileAt(outsideWindow, "business-hours")

		api := getDeployment("api")
		Expect(api.Spec.Paused).To(BeTrue())
		Expect(api.Annotations).To(HaveKeyWithValue(PausedByAnnotation, "business-hours"))
		Expect(getDeployment("worker").Spec.Paused).To(BeFalse())

		dw := getWindow("business-hours")
		Expect(dw.Status.Open).To(BeFalse())
		Expect(dw.Status.MatchedDeployments).To(Equal(int32(1)))
		Expect(dw.Status.PausedDeployments).To(Equal([]string{"api"}))
		Expect(dw.Status.NextTransition.Time.UTC()).To(Equal(time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC)))
		Expect(meta.IsStatusConditionTrue(dw.Status.Conditions, "Ready")).To(BeTrue())
		// Requeue lands on the next boundary: Tue 09:00 is 13h after Mon 20:00.
		Expect(res.RequeueAfter).To(Equal(13*time.Hour + time.Second))
	})

	It("resumes only the Deployments it paused when the window opens", func() {
		newDeployment("api", map[string]string{tierLabel: productionTier}, false)
		newDeployment("manual", map[string]string{tierLabel: productionTier}, true) // paused by a person
		newWindow("business-hours", "UTC", false)

		reconcileAt(outsideWindow, "business-hours")
		Expect(getDeployment("api").Spec.Paused).To(BeTrue())

		reconcileAt(insideWindow, "business-hours")
		api := getDeployment("api")
		Expect(api.Spec.Paused).To(BeFalse())
		Expect(api.Annotations).NotTo(HaveKey(PausedByAnnotation))
		Expect(getDeployment("manual").Spec.Paused).To(BeTrue(), "a manual pause must be kept")
		Expect(getWindow("business-hours").Status.Open).To(BeTrue())
	})

	It("applies the time zone", func() {
		newDeployment("api", map[string]string{tierLabel: productionTier}, false)
		newWindow("lagos-hours", "Africa/Lagos", false)

		// 16:30 UTC is 17:30 in Lagos: after the 17:00 close.
		reconcileAt(time.Date(2026, 11, 16, 16, 30, 0, 0, time.UTC), "lagos-hours")
		Expect(getDeployment("api").Spec.Paused).To(BeTrue())
	})

	It("does not pause anything while suspended, and releases what it paused", func() {
		newDeployment("api", map[string]string{tierLabel: productionTier}, false)
		dw := newWindow("business-hours", "UTC", false)
		reconcileAt(outsideWindow, "business-hours")
		Expect(getDeployment("api").Spec.Paused).To(BeTrue())

		dw = getWindow(dw.Name)
		dw.Spec.Suspend = true
		Expect(k8sClient.Update(ctx, dw)).To(Succeed())
		reconcileAt(outsideWindow, "business-hours")

		Expect(getDeployment("api").Spec.Paused).To(BeFalse())
		Expect(getWindow("business-hours").Status.PausedDeployments).To(BeEmpty())
	})

	It("releases a Deployment that no longer matches the selector", func() {
		newDeployment("api", map[string]string{tierLabel: productionTier}, false)
		newWindow("business-hours", "UTC", false)
		reconcileAt(outsideWindow, "business-hours")

		api := getDeployment("api")
		api.Labels[tierLabel] = "dev"
		Expect(k8sClient.Update(ctx, api)).To(Succeed())
		reconcileAt(outsideWindow, "business-hours")

		Expect(getDeployment("api").Spec.Paused).To(BeFalse())
	})

	It("resumes its Deployments when the window is deleted", func() {
		newDeployment("api", map[string]string{tierLabel: productionTier}, false)
		dw := newWindow("business-hours", "UTC", false)
		reconcileAt(outsideWindow, "business-hours")
		Expect(getDeployment("api").Spec.Paused).To(BeTrue())

		Expect(k8sClient.Delete(ctx, getWindow(dw.Name))).To(Succeed())
		reconcileAt(outsideWindow, "business-hours")

		Expect(getDeployment("api").Spec.Paused).To(BeFalse())
		err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: dw.Name}, &policyv1alpha1.DeploymentWindow{})
		Expect(errors.IsNotFound(err)).To(BeTrue(), "finalizer should be removed")
	})

	It("reports an invalid time zone and pauses nothing", func() {
		newDeployment("api", map[string]string{tierLabel: productionTier}, false)
		newWindow("bad-zone", "Mars/Olympus", false)

		reconcileAt(outsideWindow, "bad-zone")

		Expect(getDeployment("api").Spec.Paused).To(BeFalse())
		cond := meta.FindStatusCondition(getWindow("bad-zone").Status.Conditions, "Ready")
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("InvalidSpec"))
	})
})
