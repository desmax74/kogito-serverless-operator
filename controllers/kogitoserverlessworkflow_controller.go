/*
 * Copyright 2022 Red Hat, Inc. and/or its affiliates.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package controllers

import (
	"context"
	"encoding/json"
	apiv08 "github.com/davidesalerno/kogito-serverless-operator/api/v08"
	"github.com/davidesalerno/kogito-serverless-operator/builder"
	"github.com/davidesalerno/kogito-serverless-operator/converters"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// KogitoServerlessWorkflowReconciler reconciles a KogitoServerlessWorkflow object
type KogitoServerlessWorkflowReconciler struct {
	Client   client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

//+kubebuilder:rbac:groups=sw.kogito.kie.org,resources=kogitoserverlessworkflows,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=sw.kogito.kie.org,resources=kogitoserverlessworkflows/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=sw.kogito.kie.org,resources=kogitoserverlessworkflows/finalizers,verbs=update
//+kubebuilder:rbac:groups=sw.kogito.kie.org,resources=pods,verbs=get;watch;list

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// the KogitoServerlessWorkflow object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.2/pkg/reconcile
func (r *KogitoServerlessWorkflowReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("************************************************* Reconcile KogitoServerlessWorkflowReconciler")
	// Lookup the KogitoServerlessWorkflow instance for this reconcile request
	instance := &apiv08.KogitoServerlessWorkflow{}
	err := r.Client.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			log.Info("KogitoServerlessWorkflow resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get KogitoServerlessWorkflow")
		return ctrl.Result{}, err
	}

	//TODO handleBuilderSecret(instance, r.Client)
	//TODO KOGITO-7840 Add validation on Workflow Metadata
	converter := converters.NewKogitoServerlessWorkflowConverter(ctx)
	workflow, err := converter.ToCNCFWorkflow(instance)
	if err != nil {
		log.Error(err, "Failed converting KogitoServerlessWorkflow into Workflow")
		return ctrl.Result{}, err
	}
	jsonWorkflow, err := json.Marshal(workflow)
	if err != nil {
		log.Error(err, "Failed converting KogitoServerlessWorkflow into JSON")
		return ctrl.Result{}, err
	}
	buildable := builder.NewBuildable(r.Client, ctx)
	build, err := buildable.HandleSwfBuild(workflow.ID, jsonWorkflow, req)
	log.Info(string(build.Spec.BuildPhase))
	log.Info(string(build.Status.BuildPhase))
	return ctrl.Result{Requeue: true}, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *KogitoServerlessWorkflowReconciler) SetupWithManager(mgr ctrl.Manager) error {

	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv08.KogitoServerlessWorkflow{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Pod{}).
		Watches(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForOwner{
			IsController: true,
			OwnerType:    &corev1.Pod{}}).
		Complete(r)
}
