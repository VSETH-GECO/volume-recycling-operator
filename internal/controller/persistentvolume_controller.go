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
	"github.com/VSETH-GECO/volume-recycling-operator/internal/utils"
	v1 "k8s.io/api/core/v1"
	"os"
	"slices"
	"strconv"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// PersistentVolumeReconciler reconciles a PersistentVolume object
type PersistentVolumeReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	FcUtils       utils.FCUtil
	DeniedDevices []string
}

// +kubebuilder:rbac:resources=persistentvolumes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:resources=persistentvolumes/status,verbs=get;update;patch
// +kubebuilder:rbac:resources=persistentvolumes/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.0/pkg/reconcile
func (r *PersistentVolumeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	rLog := log.FromContext(ctx)

	pv := &v1.PersistentVolume{}
	err := r.Get(ctx, req.NamespacedName, pv)
	if err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	rLog.Info("Called on pv {} in phase {}", pv.Name, pv.Status.Phase)
	if pv.Status.Phase == v1.VolumeReleased {
		// We have a PV that is released - clean it and mark it as available

		if pv.Spec.FC == nil {
			rLog.Info("Released volume is not fiber channel, skipping!")
			return ctrl.Result{}, nil
		}

		var device = ""
		for _, wwn := range pv.Spec.FC.TargetWWNs {
			rLog.Info("Scanning for WWN {} LUN {}", wwn, pv.Spec.FC.Lun)
			_, dm := r.FcUtils.FindDisk(wwn, strconv.Itoa(int(*pv.Spec.FC.Lun)))
			if dm != "" {
				device = dm
				break
			}
		}

		if device == "" {
			rLog.Info("Device not found, skipping!")
			return ctrl.Result{
				Requeue:      true,
				RequeueAfter: 60 * time.Second,
			}, nil
		}

		// This is an FC device, and `device` contains the mapped local path (e.g. /dev/dm-50)
		fp, err := os.OpenFile(device, os.O_WRONLY|os.O_EXCL, 0)
		if err != nil {
			rLog.Info("Device still in use, skipping!")
			return ctrl.Result{
				Requeue:      true,
				RequeueAfter: 60 * time.Second,
			}, nil
		}
		err = fp.Close()
		if err != nil {
			rLog.Info("Couldn't release device lock, skipping!")
			return ctrl.Result{
				Requeue:      true,
				RequeueAfter: 60 * time.Second,
			}, nil
		}

		if slices.Contains(r.DeniedDevices, device) {
			rLog.Info("Device is in list of denied devices, aborting!")
			return ctrl.Result{
				Requeue:      true,
				RequeueAfter: 60 * time.Hour,
			}, nil
		}

		// Danger zone
		err = r.FcUtils.WipeDisk(device, pv.Spec.FC.FSType)
		if err != nil {
			rLog.Info("Couldn't format device, skipping!")
			return ctrl.Result{
				Requeue:      true,
				RequeueAfter: 15 * time.Minute,
			}, nil
		}

		patch := client.MergeFrom(pv.DeepCopy())
		pv.Spec.ClaimRef = nil
		err = r.Patch(ctx, pv, patch)
		if err != nil {
			rLog.Info("Couldn't update claimRef!")
			return ctrl.Result{
				Requeue: true,
			}, err
		}

		patch = client.MergeFrom(pv.DeepCopy())
		pv.Status = v1.PersistentVolumeStatus{
			Phase:   v1.VolumeAvailable,
			Message: "Wiped by operator",
		}
		err = r.Status().Patch(ctx, pv, patch)
		if err != nil {
			rLog.Info("Couldn't update status!")
			return ctrl.Result{
				Requeue: true,
			}, err
		}

		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PersistentVolumeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.PersistentVolume{}).
		Complete(r)
}
