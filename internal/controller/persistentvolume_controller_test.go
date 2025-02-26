/*
Copyright 2025 VSETH GECO

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
	"github.com/VSETH-GECO/volume-recycling-operator/internal/utils"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("PersistentVolume Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		persistentvolume := &v1.PersistentVolume{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind PersistentVolume")
			err := k8sClient.Get(ctx, typeNamespacedName, persistentvolume)
			if err != nil && errors.IsNotFound(err) {
				lun := int32(3)
				resource := &v1.PersistentVolume{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: v1.PersistentVolumeSpec{
						AccessModes: []v1.PersistentVolumeAccessMode{v1.ReadWriteOnce},
						Capacity: v1.ResourceList{
							"storage": resource.MustParse("1Gi"),
						},
						PersistentVolumeSource: v1.PersistentVolumeSource{
							FC: &v1.FCVolumeSource{
								TargetWWNs: []string{"wwnA", "wwnB"},
								Lun:        &lun,
								FSType:     "ext4",
								ReadOnly:   false,
								WWIDs:      nil,
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &v1.PersistentVolume{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance PersistentVolume")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")

			controllerReconciler := &PersistentVolumeReconciler{
				Client:  k8sClient,
				Scheme:  k8sClient.Scheme(),
				FcUtils: &stub{},
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})
})

type stub struct {
	utils.FCUtil
}

func (fUtil *stub) FindDisk(wwn, lun string) (string, string) {
	if wwn == "wwnB" && lun == "3" {
		return "disk", "/dev/TheDisk"
	}
	return "", ""
}

func (fUtil *stub) WipeDisk(path string, filesystem string) error {
	if filesystem == "ext4" && path == "/dev/TheDisk" {
		return nil
	}
	return fmt.Errorf("incorrect call to wipe disk %s %s", path, filesystem)
}
