// Copyright 2023 Red Hat, Inc. and/or its affiliates
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package profiles

import (
	"context"
	"fmt"
	"time"

	"k8s.io/klog/v2"

	"github.com/kiegroup/kogito-serverless-operator/log"

	"k8s.io/client-go/rest"

	"github.com/kiegroup/kogito-serverless-operator/api/metadata"
	"github.com/kiegroup/kogito-serverless-operator/controllers/workflowdef"
	"github.com/kiegroup/kogito-serverless-operator/utils"
	kubeutil "github.com/kiegroup/kogito-serverless-operator/utils/kubernetes"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kiegroup/kogito-serverless-operator/api"
	operatorapi "github.com/kiegroup/kogito-serverless-operator/api/v1alpha08"
	api2 "github.com/kiegroup/kogito-serverless-operator/container-builder/api"
	"github.com/kiegroup/kogito-serverless-operator/controllers/builder"
	"github.com/kiegroup/kogito-serverless-operator/controllers/platform"
)

var _ ProfileReconciler = &prodProfile{}

type prodProfile struct {
	baseReconciler
}

const (
	requeueAfterStartingBuild   = 3 * time.Minute
	requeueWhileWaitForBuild    = 1 * time.Minute
	requeueWhileWaitForPlatform = 5 * time.Second

	quarkusProdConfigMountPath = "/deployments/config"
)

// prodObjectEnsurers is a struct for the objects that ReconciliationState needs to create in the platform for the Production profile.
// ReconciliationState that needs access to it must include this struct as an attribute and initialize it in the profile builder.
// Use newProdObjectEnsurers to facilitate building this struct
type prodObjectEnsurers struct {
	deployment          ObjectEnsurer
	service             ObjectEnsurer
	propertiesConfigMap ObjectEnsurer
}

func newProdObjectEnsurers(support *stateSupport) *prodObjectEnsurers {
	return &prodObjectEnsurers{
		deployment:          newDefaultObjectEnsurer(support.client, defaultDeploymentCreator),
		service:             newDefaultObjectEnsurer(support.client, defaultServiceCreator),
		propertiesConfigMap: newDefaultObjectEnsurer(support.client, workflowPropsConfigMapCreator),
	}
}

func newProdProfileReconciler(client client.Client, config *rest.Config, recorder record.EventRecorder) ProfileReconciler {
	support := &stateSupport{
		client:   client,
		recorder: recorder,
	}
	// the reconciliation state machine
	stateMachine := newReconciliationStateMachine(
		&newBuilderReconciliationState{stateSupport: support},
		&followBuildStatusReconciliationState{stateSupport: support},
		&deployWorkflowReconciliationState{stateSupport: support, ensurers: newProdObjectEnsurers(support)},
	)
	reconciler := &prodProfile{
		baseReconciler: newBaseProfileReconciler(support, stateMachine),
	}

	return reconciler
}

func (p prodProfile) GetProfile() metadata.ProfileType {
	return metadata.ProdProfile
}

type newBuilderReconciliationState struct {
	*stateSupport
}

func (h *newBuilderReconciliationState) CanReconcile(workflow *operatorapi.SonataFlow) bool {
	return workflow.Status.GetTopLevelCondition().IsUnknown() ||
		workflow.Status.IsWaitingForPlatform() ||
		workflow.Status.IsBuildFailed() ||
		workflow.Status.IsWaitingConfigurationChanges()
}

func (h *newBuilderReconciliationState) Do(ctx context.Context, workflow *operatorapi.SonataFlow) (ctrl.Result, []client.Object, error) {
	klog.V(log.I).Info("NewBuilderReconciliationState Do")
	//check the platform for the first time, we are here because in the CanReconcile the top level is unknow or we are WaitingForPlatform
	activePlatform, build, cm, err := getContextObjects(ctx, workflow, h.stateSupport)
	if activePlatform == nil {
		return ctrl.Result{RequeueAfter: requeueWhileWaitForPlatform}, nil, err
	}

	// ...let's check before if we have got already a build!
	if build == nil {
		return ctrl.Result{}, nil, err
	}
	updatedCM := false
	// we store the dockerfile because the configmap doesn't have a generation version and the version is updated often even if the content of the data fields aren't updated
	valCm := cm.Data[builder.ConfigDockerfile]
	if workflow.Status.ObserverdDockerfile != valCm {
		updatedCM = true
	}

	updatedPlatform := false
	if workflow.Status.ObservedPlatformGeneration != activePlatform.Generation && workflow.Status.ObservedPlatformGeneration != 0 {
		updatedPlatform = true
	}

	workflow.Status.ObservedPlatformGeneration = activePlatform.Generation
	workflow.Status.ObserverdDockerfile = valCm

	if build.Status.BuildPhase != operatorapi.BuildPhaseFailed {
		//Double check on the innerbuild
		innerBuildPhase, stringError := getInnerBuild(build)
		if string(innerBuildPhase) == string(operatorapi.BuildPhaseFailed) {
			workflow.Status.Manager().MarkFalse(api.BuiltConditionType, api.BuildFailedReason, stringError)
			workflow.Status.Manager().MarkFalse(api.RunningConditionType, api.WaitingForBuildReason, stringError)
		} else {
			workflow.Status.Manager().MarkFalse(api.BuiltConditionType, api.BuildIsRunningReason, "")
			workflow.Status.Manager().MarkFalse(api.RunningConditionType, api.WaitingForBuildReason, "")
		}
		klog.V(log.I).Info("NewBuilderReconciliationState Do Perform StatusUpdate")
		_, err := h.performStatusUpdate(ctx, workflow)
		return ctrl.Result{RequeueAfter: requeueAfterStartingBuild}, nil, err
	} else {
		//We track the number of failed builds to see if we are in the configured range
		if updatedCM || updatedPlatform {
			workflow.Status.ObserverdDockerfile = cm.Data[builder.ConfigDockerfile]
			workflow.Status.ObservedPlatformGeneration = activePlatform.Generation
			restartBuild(ctx, h.stateSupport, workflow, activePlatform, build)
			if err != nil {
				klog.V(log.E).ErrorS(err, "Error updating workflow status newBuilderReconciliationState")
			}
			_, err := h.performStatusUpdate(ctx, workflow)
			return ctrl.Result{RequeueAfter: requeueAfterStartingBuild}, nil, err
		} else if !handleMultipleBuildsAfterError(ctx, build, workflow, *h.stateSupport, *activePlatform) {
			//We have surpassed the number of failed builds configured, we are going to change the condition to WaitingForChanges from the user
			msgFinal := fmt.Sprintf(" Build is in failed state, stop to build after %v attempts and waiting to fix the problem. Checks the pod logs and try to fix the problem on platform or on Dockerfile or delete the SonataFlowBuild to restart a new build cycle", build.Status.BuildAttemptsAfterError)
			klog.V(log.I).InfoS(msgFinal)
			h.recorder.Event(workflow, v1.EventTypeWarning, "SonataFlowBuild Error", msgFinal)
			workflow.Status.ObservedPlatformGeneration = activePlatform.Generation
			_, err := h.performStatusUpdate(ctx, workflow)
			return ctrl.Result{}, nil, err
		}
		_, err := h.performStatusUpdate(ctx, workflow)
		return ctrl.Result{RequeueAfter: requeueAfterStartingBuild}, nil, err
	}
}

type followBuildStatusReconciliationState struct {
	*stateSupport
}

func (h *followBuildStatusReconciliationState) CanReconcile(workflow *operatorapi.SonataFlow) bool {
	value := workflow.Status.IsBuildRunningOrUnknown()
	return value
}

func (h *followBuildStatusReconciliationState) Do(ctx context.Context, workflow *operatorapi.SonataFlow) (ctrl.Result, []client.Object, error) {
	// Let's retrieve the build to check the status
	klog.V(log.I).Info("FollowBuildStatusReconciliationState Do")
	buildManager := builder.NewSonataFlowBuildManager(ctx, h.client)
	build, err := buildManager.GetOrCreateBuild(workflow)
	if err != nil {
		// Can't recover, signal on event and on log
		klog.V(log.E).ErrorS(err, fmt.Sprintf("Failed to get or create the build for the workflow, reason: %v error: %v", build.Status.Error, err))
		h.stateSupport.recorder.Event(workflow, v1.EventTypeWarning, "SonataFlowGetOrCreateBuild Error", fmt.Sprintf("Failed to get or create the build for the workflow, reason: %v error: %v", build.Status.Error, err))
		workflow.Status.Manager().MarkFalse(api.BuiltConditionType, api.BuildFailedReason, build.Status.Error)
		_, err := h.performStatusUpdate(ctx, workflow)
		return ctrl.Result{RequeueAfter: requeueAfterFailure}, nil, err
	}

	if build.Status.BuildPhase == operatorapi.BuildPhaseSucceeded {
		// Double check on the builder pod
		containerBuild := &api2.ContainerBuild{}
		build.Status.GetInnerBuild(containerBuild)
		phaseCurrent := containerBuild.Status.Phase
		if string(phaseCurrent) == string(operatorapi.BuildPhaseFailed) {
			workflow.Status.Manager().MarkFalse(api.BuiltConditionType, api.BuildFailedReason, containerBuild.Status.Error)
		} else {
			//If we have finished a build and the workflow is not running, we will start the provisioning phase
			workflow.Status.Manager().MarkTrue(api.BuiltConditionType)
		}
		_, err = h.performStatusUpdate(ctx, workflow)
	} else if build.Status.BuildPhase == operatorapi.BuildPhaseFailed ||
		build.Status.BuildPhase == operatorapi.BuildPhaseError {
		activePlatform, err := getActivePlatform(ctx, workflow, h.client, h.stateSupport)
		if err != nil {
			return ctrl.Result{RequeueAfter: requeueWhileWaitForPlatform}, nil, err
		}

		if workflow.Status.GetCondition(api.BuiltConditionType).IsFalse() &&
			workflow.Status.GetCondition(api.BuiltConditionType).Reason == api.WaitingForWrongConfigurationReason {
			restartBuild(ctx, h.stateSupport, workflow, activePlatform, build)
		} else {
			//We track the number of failed builds to see if we are in the configured range
			if handleMultipleBuildsAfterError(ctx, build, workflow, *h.stateSupport, *activePlatform) {
				_, err := h.performStatusUpdate(ctx, workflow)
				return ctrl.Result{}, nil, err
			}
		}
	}
	return ctrl.Result{RequeueAfter: requeueWhileWaitForBuild}, nil, nil
}

type deployWorkflowReconciliationState struct {
	*stateSupport
	ensurers           *prodObjectEnsurers
	deploymentVisitors []mutateVisitor
}

func (h *deployWorkflowReconciliationState) CanReconcile(workflow *operatorapi.SonataFlow) bool {
	return workflow.Status.GetCondition(api.BuiltConditionType).IsTrue()
}

func (h *deployWorkflowReconciliationState) Do(ctx context.Context, workflow *operatorapi.SonataFlow) (ctrl.Result, []client.Object, error) {
	klog.V(log.I).Info("DeployWorkflowReconciliationState Do")
	pl, err := platform.GetActivePlatform(ctx, h.client, workflow.Namespace)
	if err != nil {
		msg := "No active Platform for namespace %s so the resWorkflowDef cannot be deployed. Waiting for an active platform"
		workflow.Status.Manager().MarkFalse(api.RunningConditionType, api.WaitingForPlatformReason, msg, workflow.Namespace)
		h.stateSupport.recorder.Event(workflow, v1.EventTypeWarning, "SonataFlowPlatformError", fmt.Sprintf(msg, workflow.Namespace))
		_, err := h.performStatusUpdate(ctx, workflow)
		return ctrl.Result{RequeueAfter: requeueWhileWaitForPlatform}, nil, err
	}

	if h.isWorkflowChanged(workflow) { // Let's check that the 2 resWorkflowDef definition are different
		workflow.Status.Manager().MarkUnknown(api.RunningConditionType, "", "")
		buildManager := builder.NewSonataFlowBuildManager(ctx, h.client)
		build, err := buildManager.GetOrCreateBuildWithPlatform(pl, workflow)
		if err != nil {
			h.stateSupport.recorder.Event(workflow, v1.EventTypeWarning, "SonataFlowBuildManagerError", fmt.Sprintf("Error: %s ", err))
			return ctrl.Result{}, nil, err
		}
		if err = buildManager.MarkToRestart(build); err != nil {
			h.stateSupport.recorder.Event(workflow, v1.EventTypeWarning, "SonataFlowBuildManagerError", fmt.Sprintf("Error: %s ", err))
			return ctrl.Result{}, nil, err
		}

		workflow.Status.Manager().MarkFalse(api.BuiltConditionType, api.BuildIsRunningReason, "Marked to restart")
		workflow.Status.Manager().MarkUnknown(api.RunningConditionType, "", "")
		_, err = h.performStatusUpdate(ctx, workflow)
		return ctrl.Result{Requeue: false}, nil, err
	}

	// didn't change, business as usual
	image := workflowdef.GetWorkflowAppImageNameTag(workflow)
	if len(pl.Spec.BuildPlatform.Registry.Address) > 0 {
		image = pl.Spec.BuildPlatform.Registry.Address + "/" + image
	}
	return h.handleObjects(ctx, workflow, image)
}

func (h *deployWorkflowReconciliationState) handleObjects(ctx context.Context, workflow *operatorapi.SonataFlow, image string) (reconcile.Result, []client.Object, error) {
	// the dev one is ok for now
	propsCM, _, err := h.ensurers.propertiesConfigMap.ensure(ctx, workflow, ensureProdWorkflowPropertiesConfigMapMutator(workflow))
	if err != nil {
		return ctrl.Result{}, nil, err
	}

	// Check if this Deployment already exists
	// TODO: we should NOT do this. The ensurers are there to do exactly this fetch. Review once we refactor this reconciliation algorithm. See https://issues.redhat.com/browse/KOGITO-8524
	existingDeployment := &appsv1.Deployment{}
	requeue := false
	if err := h.client.Get(ctx, client.ObjectKeyFromObject(workflow), existingDeployment); err != nil {
		if !errors.IsNotFound(err) {
			h.stateSupport.recorder.Event(existingDeployment, v1.EventTypeWarning, "SonataFlowDeploymentError", fmt.Sprintf("Error: %s ", err))
			return reconcile.Result{Requeue: false}, nil, err
		}
		deployment, _, err :=
			h.ensurers.deployment.ensure(
				ctx,
				workflow,
				h.getDeploymentMutateVisitors(workflow, image, propsCM.(*v1.ConfigMap))...,
			)
		if err != nil {
			h.stateSupport.recorder.Event(deployment, v1.EventTypeWarning, "SonataFlowDeploymentError", fmt.Sprintf("Error: %s ", err))
			return reconcile.Result{}, nil, err
		}
		existingDeployment, _ = deployment.(*appsv1.Deployment)
		requeue = true
	}
	// TODO: verify if deployment is ready. See https://issues.redhat.com/browse/KOGITO-8524

	existingService := &v1.Service{}
	if err := h.client.Get(ctx, client.ObjectKeyFromObject(workflow), existingService); err != nil {
		if !errors.IsNotFound(err) {
			return reconcile.Result{Requeue: false}, nil, err
		}
		service, _, err := h.ensurers.service.ensure(ctx, workflow, defaultServiceMutateVisitor(workflow))
		if err != nil {
			return reconcile.Result{}, nil, err
		}
		existingService, _ = service.(*v1.Service)
		requeue = true
	}
	// TODO: verify if service is ready. See https://issues.redhat.com/browse/KOGITO-8524

	objs := []client.Object{existingDeployment, existingService, propsCM}

	if !requeue {
		klog.V(log.I).InfoS("Skip reconcile: Deployment and service already exists",
			"Deployment.Namespace", existingDeployment.Namespace, "Deployment.Name", existingDeployment.Name)
		// TODO: very naive, the state should observe the Deployment's status: https://issues.redhat.com/browse/KOGITO-8524

		workflow.Status.Manager().MarkTrue(api.RunningConditionType)
		klog.V(log.I).Info("1010101001")
		if _, err := h.performStatusUpdate(ctx, workflow); err != nil {
			return reconcile.Result{Requeue: false}, nil, err
		}

		return reconcile.Result{RequeueAfter: requeueAfterIsRunning}, objs, nil
	}

	workflow.Status.Manager().MarkFalse(api.RunningConditionType, api.WaitingForDeploymentReason, "")
	if _, err := h.performStatusUpdate(ctx, workflow); err != nil {
		h.stateSupport.recorder.Event(workflow, v1.EventTypeWarning, "SonataFlowStatusUpdateError", fmt.Sprintf("Error: %s ", err))
		return reconcile.Result{Requeue: false}, nil, err
	}

	return reconcile.Result{RequeueAfter: requeueAfterFollowDeployment}, objs, nil
}

// getDeploymentMutateVisitors gets the deployment mutate visitors based on the current plat
func (h *deployWorkflowReconciliationState) getDeploymentMutateVisitors(workflow *operatorapi.SonataFlow, image string, configMap *v1.ConfigMap) []mutateVisitor {
	if utils.IsOpenShift() {
		return []mutateVisitor{defaultDeploymentMutateVisitor(workflow),
			mountProdConfigMapsMutateVisitor(configMap),
			addOpenShiftImageTriggerDeploymentMutateVisitor(image),
			naiveApplyImageDeploymentMutateVisitor(image)}
	}
	return []mutateVisitor{defaultDeploymentMutateVisitor(workflow),
		naiveApplyImageDeploymentMutateVisitor(image),
		mountProdConfigMapsMutateVisitor(configMap)}
}

// isWorkflowChanged marks the workflow status as unknown to require a new build reconciliation
func (h *deployWorkflowReconciliationState) isWorkflowChanged(workflow *operatorapi.SonataFlow) bool {
	generation := kubeutil.GetLastGeneration(workflow.Namespace, workflow.Name, h.client, context.TODO())
	if generation > workflow.Status.ObservedGeneration {
		return true
	}
	return false
}
