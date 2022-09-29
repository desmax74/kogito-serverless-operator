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

package v08

import (
	"github.com/RHsyseng/operator-utils/pkg/olm"
	"github.com/ricardozanini/kogito-builder/api"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// KogitoServerlessBuildSpec defines the desired state of KogitoServerlessBuild
type KogitoServerlessBuildSpec struct {
	WorkflowId    string         `json:"workflowId,omitempty"`
	ImageName     string         `json:"imageName,omitempty"`
	PodMiddleName string         `json:"podMiddleName,omitempty"`
	Dockerfile    []byte         `json:"dockerfile,omitempty"`
	BuildPhase    api.BuildPhase `json:"buildPhase,omitempty"`
}

// KogitoServerlessBuildStatus defines the observed state of KogitoServerlessBuild
type KogitoServerlessBuildStatus struct {
	WorkflowId  string                    `json:"workflowId,omitempty"`
	Deployments olm.DeploymentStatus      `json:"deployments"`
	Phase       ConditionType             `json:"phase,omitempty"`
	Applied     KogitoServerlessBuildSpec `json:"applied,omitempty"`
	Version     string                    `json:"version,omitempty"`
	BuildPhase  api.BuildPhase            `json:"buildPhase,omitempty"`
	Builder     api.Build                 `json:"builder,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// +kubebuilder:subresource:status
// KogitoServerlessBuild is the Schema for the kogitoserverlessbuilds API
type KogitoServerlessBuild struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KogitoServerlessBuildSpec   `json:"spec,omitempty"`
	Status KogitoServerlessBuildStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +kubebuilder:object:root=true
// +kubebuilder:object:generate=true
// KogitoServerlessBuildList is the Schema for the kogitoserverlessbuildsList API
type KogitoServerlessBuildList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KogitoServerlessBuild `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KogitoServerlessBuild{}, &KogitoServerlessBuildList{})
}
