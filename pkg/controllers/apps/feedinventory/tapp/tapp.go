/*
Copyright 2022 The Clusternet Authors.

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

package tapp

import (
	"encoding/json"
	"strconv"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	tappv1 "tkestack.io/tapp/pkg/apis/tappcontroller/v1"

	appsapi "github.com/clusternet/clusternet/pkg/apis/apps/v1alpha1"
	"github.com/clusternet/clusternet/pkg/controllers/apps/feedinventory/utils"
)

type Plugin struct {
	name string
}

func NewPlugin() *Plugin {
	return &Plugin{
		name: "tapp",
	}
}

func (pl *Plugin) Parser(rawData []byte) (*int32, appsapi.ReplicaRequirements, string, error) {
	// use apps.tkestack.io/v1 for deserialization
	// all the fields that we needed are backward compatible with apps.tkestack.io/v1
	tapp := &tappv1.TApp{}

	if err := json.Unmarshal(rawData, tapp); err != nil {
		return nil, appsapi.ReplicaRequirements{}, "", err
	}

	template := GetPodTemplateFromTApp(&tapp.Spec, int(tapp.Spec.Replicas-1))
	return &tapp.Spec.Replicas, utils.GetReplicaRequirements(template.Spec), "/spec/replicas", nil
}

func (pl *Plugin) Name() string {
	return pl.name
}

func (pl *Plugin) Kind() string {
	return "TApp"
}

func GetPodTemplateNameFromTApp(spec *tappv1.TAppSpec, id int) string {
	defaultTemplateName := spec.DefaultTemplateName
	if defaultTemplateName == "" {
		defaultTemplateName = tappv1.DefaultTemplateName
	}
	idStr := strconv.Itoa(id)
	if name, exist := spec.Templates[idStr]; !exist {
		return defaultTemplateName
	} else {
		return name
	}
}

func GetPodTemplateFromTApp(spec *tappv1.TAppSpec, id int) *corev1.PodTemplateSpec {
	templateName := GetPodTemplateNameFromTApp(spec, id)
	return GetPodTemplateWithNameFromTApp(spec, templateName)
}

func GetPodTemplateWithNameFromTApp(spec *tappv1.TAppSpec, templateName string) *corev1.PodTemplateSpec {
	if templateName == tappv1.DefaultTemplateName {
		return &spec.Template
	} else if template, ok := spec.TemplatePool[templateName]; ok {
		return &template
	} else {
		klog.Errorf("template %v not found in templatePool which is need by some pods, use default")
		return &spec.Template
	}
}
