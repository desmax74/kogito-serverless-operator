// Copyright 2023 Red Hat, Inc. and/or its affiliates
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package builder

import (
	"os"
	"strings"
	"testing"

	"github.com/kiegroup/kogito-serverless-operator/version"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorapi "github.com/kiegroup/kogito-serverless-operator/api/v1alpha08"

	"github.com/kiegroup/kogito-serverless-operator/utils"
)

func Test_replaceBuilderImageVersionPlaceholder(t *testing.T) {

	b, err := os.ReadFile("../config/manager/kogito_builder_dockerfile.yaml")
	if err != nil {
		assert.NoError(t, err)
	}

	platform := operatorapi.KogitoServerlessPlatform{
		Spec: operatorapi.KogitoServerlessPlatformSpec{
			BuilderImageVersion: version.BuilderImageVersion,
		},
	}
	cmData := make(map[string]string)
	cmData[resourceDockerfile] = string(b)
	cmData[configKeyDefaultExtension] = ".sw.json"
	cmData[configKeyDefaultBuilderResourceName] = "Dockerfile"
	configMap := createConfigMapBase("Test_replaceBuilderImageVersionPlaceholder", "manager-config", cmData)
	placeholders := CommonConfigMapPlaceholders{BuilderImageVersion: platform.Spec.BuilderImageVersion}
	error := isValidBuilderCommonConfigMap(&configMap, placeholders)
	assert.NoError(t, error)
	valueAfterValidation := configMap.Data[configMap.Data[configKeyDefaultBuilderResourceName]]
	assert.False(t, strings.Contains(valueAfterValidation, "{{"))
	assert.False(t, strings.Contains(valueAfterValidation, "}}"))
	assert.True(t, strings.Contains(valueAfterValidation, "quay.io/kiegroup/kogito-swf-builder"+":"+version.BuilderImageVersion))
}

func createConfigMapBase(namespace string, name string, cmData map[string]string) corev1.ConfigMap {
	cm := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Immutable: utils.Pbool(false),
		Data:      cmData,
	}
	return cm
}
