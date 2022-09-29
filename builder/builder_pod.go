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
package builder

import (
	"context"
	apiv08 "github.com/davidesalerno/kogito-serverless-operator/api/v08"
	"github.com/davidesalerno/kogito-serverless-operator/constants"
	"github.com/go-logr/logr"
	"github.com/ricardozanini/kogito-builder/api"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var log logr.Logger

type BuildableSwf struct {
	client client.Client
	log    logr.Logger
	Ctx    context.Context
}

func NewBuildableSwf(client client.Client,
	Logger logr.Logger,
	Ctx context.Context) BuildableSwf {
	return BuildableSwf{
		log:    Logger,
		Ctx:    Ctx,
		client: client,
	}
}

func (buildable *BuildableSwf) GetBuild(ctx context.Context, req ctrl.Request, workflowID string, client client.Client) (apiv08.KogitoServerlessBuild, error) {
	buildInstance := &apiv08.KogitoServerlessBuild{}
	buildInstance.Spec.SwfName = workflowID
	error := client.Get(ctx, types.NamespacedName{Namespace: req.NamespacedName.Name, Name: workflowID}, buildInstance)
	return *buildInstance, error
}

func (buildable *BuildableSwf) CreateBuild(workflowID string, workflowSwf []byte) (apiv08.KogitoServerlessBuild, error) {
	buildInstance := &apiv08.KogitoServerlessBuild{}
	buildInstance.Spec.SwfName = workflowID
	buildInstance.Spec.SourceSwf = workflowSwf
	buildInstance.ObjectMeta.Namespace = constants.BUILDER_NAMESPACE_DEFAULT
	buildInstance.ObjectMeta.Name = workflowID
	error := buildable.client.Create(buildable.Ctx, buildInstance)
	return *buildInstance, error
}

func (buildable *BuildableSwf) HandleBuildStatus(workflowID string, yamlWorkflow []byte, req ctrl.Request) (ctrl.Result, error) {
	//buildable.InjectClient(buildable.client)
	buildInstance, error := buildable.GetBuild(buildable.Ctx, req, workflowID, buildable.client)
	if error != nil {
		buildInstance, error = buildable.CreateBuild(workflowID, yamlWorkflow)
	}
	return buildable.UpdateBuildStatus(buildInstance)
}

func (buildable *BuildableSwf) UpdateBuildStatus(buildInstance apiv08.KogitoServerlessBuild) (ctrl.Result, error) {
	log.Info("UpdateBuildStatus")
	//@TODO handle events from the Build controller
	if buildInstance.Status.BuildPhase == api.BuildPhaseSucceeded {
		log.Info("Build Status succeed")
		return ctrl.Result{}, nil
	}

	if buildInstance.Status.BuildPhase == api.BuildPhaseScheduling {
		log.Info("Build Status scheduling")
		return ctrl.Result{}, nil
	}

	if buildInstance.Status.BuildPhase == api.BuildPhaseFailed {
		log.Info("Build Status failed")
		return ctrl.Result{}, nil
	}

	if buildInstance.Status.BuildPhase == api.BuildPhaseError {
		log.Info("Build Status error")
		return ctrl.Result{}, nil
	}

	if buildInstance.Status.BuildPhase == api.BuildPhaseInitialization {
		log.Info("Build Status initialization")
		return ctrl.Result{}, nil
	}

	if buildInstance.Status.BuildPhase == api.BuildPhaseInterrupted {
		log.Info("Build Status interrupted")
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, nil
}
