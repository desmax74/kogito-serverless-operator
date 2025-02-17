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

package common

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/apache/incubator-kie-kogito-serverless-operator/api"
	operatorapi "github.com/apache/incubator-kie-kogito-serverless-operator/api/v1alpha08"
	"github.com/apache/incubator-kie-kogito-serverless-operator/log"
	kubeutil "github.com/apache/incubator-kie-kogito-serverless-operator/utils/kubernetes"
)

var _ WorkflowDeploymentHandler = &deploymentHandler{}

// WorkflowDeploymentHandler interface to handle workflow deployment features.
type WorkflowDeploymentHandler interface {
	// SyncDeploymentStatus updates the workflow status aligned with the deployment counterpart.
	// For example, if the deployment is in a failed state, it sets the status to
	// Running `false` and the Message and Reason to human-readable format.
	SyncDeploymentStatus(ctx context.Context, workflow *operatorapi.SonataFlow) (ctrl.Result, error)
}

// DeploymentHandler creates a new WorkflowDeploymentHandler implementation based on the current profile.
func DeploymentHandler(c client.Client) WorkflowDeploymentHandler {
	return &deploymentHandler{c: c}
}

type deploymentHandler struct {
	c client.Client
}

func (d deploymentHandler) SyncDeploymentStatus(ctx context.Context, workflow *operatorapi.SonataFlow) (ctrl.Result, error) {
	deployment := &appsv1.Deployment{}
	if err := d.c.Get(ctx, client.ObjectKeyFromObject(workflow), deployment); err != nil {
		// we should have the deployment by this time, so even if the error above is not found, we should halt.
		workflow.Status.Manager().MarkFalse(api.RunningConditionType, api.DeploymentUnavailableReason, "Couldn't find the workflow deployment")
		return ctrl.Result{RequeueAfter: RequeueAfterFailure}, err
	}

	// Deployment is available, we can return after setting Running = TRUE
	if kubeutil.IsDeploymentAvailable(deployment) {
		workflow.Status.Manager().MarkTrue(api.RunningConditionType)
		klog.V(log.I).InfoS("Workflow is in Running Condition")
		return ctrl.Result{RequeueAfter: RequeueAfterIsRunning}, nil
	}

	if kubeutil.IsDeploymentFailed(deployment) {
		// Fallback to a general failure message if we can't determine if the deployment has minimum replicas available.
		failedReason := GetDeploymentUnavailabilityMessage(deployment)
		workflow.Status.LastTimeRecoverAttempt = metav1.Now()
		workflow.Status.Manager().MarkFalse(api.RunningConditionType, api.DeploymentFailureReason, failedReason)
		klog.V(log.I).InfoS("Workflow deployment failed", "Reason Message", failedReason)
		return ctrl.Result{RequeueAfter: RequeueAfterFailure}, nil
	}

	// Deployment hasn't minimum replicas, let's find out why to give users a meaningful information
	if kubeutil.IsDeploymentMinimumReplicasUnavailable(deployment) {
		message, err := kubeutil.DeploymentTroubleshooter(d.c, deployment, operatorapi.DefaultContainerName).ReasonMessage()
		if err != nil {
			return ctrl.Result{RequeueAfter: RequeueAfterFailure}, err
		}
		if len(message) > 0 {
			klog.V(log.I).InfoS("Workflow is not in Running condition duo to a deployment unavailability issue", "reason", message)
			workflow.Status.Manager().MarkFalse(api.RunningConditionType, api.DeploymentUnavailableReason, message)
			return ctrl.Result{RequeueAfter: RequeueAfterFailure}, nil
		}
	}

	workflow.Status.Manager().MarkFalse(api.RunningConditionType, api.WaitingForDeploymentReason, "")
	klog.V(log.I).InfoS("Workflow is in WaitingForDeployment Condition")
	return ctrl.Result{RequeueAfter: RequeueAfterFollowDeployment, Requeue: true}, nil
}

// GetDeploymentUnavailabilityMessage gets the replica failure reason.
// MUST be called after checking that the Deployment is NOT available.
// If there's no reason, the Deployment state has no apparent reason to be in failed state.
func GetDeploymentUnavailabilityMessage(deployment *appsv1.Deployment) string {
	failure := kubeutil.GetDeploymentUnavailabilityMessage(deployment)
	if len(failure) == 0 {
		failure = fmt.Sprintf("Workflow Deployment %s is unavailable", deployment.Name)
	}
	return failure
}
