/*
Copyright 2022.
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

package controllers

import (
	"context"
	"fmt"
	"github.com/davidesalerno/kogito-serverless-operator/builder"
	"github.com/ricardozanini/kogito-builder/api"
	buildr "github.com/ricardozanini/kogito-builder/builder"
	clientr "github.com/ricardozanini/kogito-builder/client"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	api08 "github.com/davidesalerno/kogito-serverless-operator/api/v08"
)

// KogitoServerlessBuildReconciler reconciles a KogitoServerlessBuild object
type KogitoServerlessBuildReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=sw.kogito.kie.org,resources=kogitoserverlessbuilds,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=sw.kogito.kie.org,resources=kogitoserverlessbuilds/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=sw.kogito.kie.org,resources=kogitoserverlessbuilds/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the KogitoServerlessBuild object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.12.1/pkg/reconcile
func (r *KogitoServerlessBuildReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := log.FromContext(ctx)
	log.Info("Reconcile KogitoServerlessBuildReconciler")
	instance := &api08.KogitoServerlessBuild{}

	err := r.Get(ctx, req.NamespacedName, instance)
	if err == nil {
		log.Info("Build CR present")
		phase := instance.Spec.BuildPhase
		log.Info(string(phase))
		if phase != api.BuildPhaseSucceeded && phase != api.BuildPhaseError && phase != api.BuildPhaseFailed && phase != api.BuildPhasePending {
			builder := builder.NewBuilder(ctx)
			build, err := builder.BuildImageWithDefaults(instance.Spec.SwfName, instance.Spec.SourceSwf)
			if err == nil {
				cli, err := clientr.NewClient(true)
				if err == nil {
					build, err := buildr.FromBuild(build).WithClient(cli).Reconcile()
					if err == nil {
						instance.Spec.BuildPhase = build.Status.Phase
						err := r.Update(ctx, instance)
						if err == nil {
							log.Info("create an event")
							r.Recorder.Event(instance, corev1.EventTypeNormal, "Updated", fmt.Sprintf("Updated buildphase to  %s", instance.Spec.BuildPhase))
							return ctrl.Result{Requeue: true}, nil
						}
					}
				}
			}
			log.Error(err, "error")
		}
	} else {
		log.Info("Build CR isn't present")
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KogitoServerlessBuildReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&api08.KogitoServerlessBuild{}).
		Watches(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
