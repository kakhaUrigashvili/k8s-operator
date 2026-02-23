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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	webappv1 "github.com/kurigashvili/k8s-operator/api/v1"
)

var _ = Describe("Website Controller", func() {
	const resourceName = "test-site"
	const namespace = "default"

	ctx := context.Background()

	namespacedName := types.NamespacedName{
		Name:      resourceName,
		Namespace: namespace,
	}

	Context("When creating a Website resource", func() {
		BeforeEach(func() {
			By("creating the Website CR")
			website := &webappv1.Website{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Spec: webappv1.WebsiteSpec{
					Image:    "nginx:1.25",
					Replicas: ptr.To(int32(2)),
					Port:     8080,
				},
			}
			err := k8sClient.Get(ctx, namespacedName, &webappv1.Website{})
			if errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, website)).To(Succeed())
			}
		})

		AfterEach(func() {
			By("cleaning up the Website CR")
			website := &webappv1.Website{}
			err := k8sClient.Get(ctx, namespacedName, website)
			if err == nil {
				Expect(k8sClient.Delete(ctx, website)).To(Succeed())
			}
		})

		It("should create a Deployment and Service", func() {
			By("running reconcile")
			reconciler := &WebsiteReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: namespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))

			By("checking the Deployment was created")
			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, namespacedName, &dep)).To(Succeed())
			Expect(*dep.Spec.Replicas).To(Equal(int32(2)))
			Expect(dep.Spec.Template.Spec.Containers).To(HaveLen(1))
			Expect(dep.Spec.Template.Spec.Containers[0].Image).To(Equal("nginx:1.25"))
			Expect(dep.Spec.Template.Spec.Containers[0].Ports[0].ContainerPort).To(Equal(int32(8080)))

			By("checking owner reference is set on Deployment")
			Expect(dep.OwnerReferences).To(HaveLen(1))
			Expect(dep.OwnerReferences[0].Kind).To(Equal("Website"))
			Expect(dep.OwnerReferences[0].Name).To(Equal(resourceName))

			By("checking the Service was created")
			var svc corev1.Service
			Expect(k8sClient.Get(ctx, namespacedName, &svc)).To(Succeed())
			Expect(svc.Spec.Type).To(Equal(corev1.ServiceTypeClusterIP))
			Expect(svc.Spec.Ports).To(HaveLen(1))
			Expect(svc.Spec.Ports[0].Port).To(Equal(int32(8080)))

			By("checking owner reference is set on Service")
			Expect(svc.OwnerReferences).To(HaveLen(1))
			Expect(svc.OwnerReferences[0].Kind).To(Equal("Website"))
		})

		It("should update the Deployment when spec changes", func() {
			reconciler := &WebsiteReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			By("running initial reconcile")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: namespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("updating the Website spec")
			var website webappv1.Website
			Expect(k8sClient.Get(ctx, namespacedName, &website)).To(Succeed())
			website.Spec.Replicas = ptr.To(int32(3))
			website.Spec.Image = "nginx:1.26"
			Expect(k8sClient.Update(ctx, &website)).To(Succeed())

			By("running reconcile again")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: namespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("verifying the Deployment was updated")
			var dep appsv1.Deployment
			Expect(k8sClient.Get(ctx, namespacedName, &dep)).To(Succeed())
			Expect(*dep.Spec.Replicas).To(Equal(int32(3)))
			Expect(dep.Spec.Template.Spec.Containers[0].Image).To(Equal("nginx:1.26"))
		})
	})

	Context("When the Website resource does not exist", func() {
		It("should not return an error", func() {
			reconciler := &WebsiteReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			result, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "nonexistent",
					Namespace: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})
	})
})
