/*
Copyright 2021 The Kubernetes Authors.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

// GoofyMachineTemplateSpec defines the desired state of GoofyMachineTemplate.
type GoofyMachineTemplateSpec struct {
	Template GoofyMachineTemplateResource `json:"template"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=goofymachinetemplates,scope=Namespaced,categories=cluster-api
// +kubebuilder:storageversion
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Time duration since creation of GoofyMachineTemplate"

// GoofyMachineTemplate is the Schema for the goofymachinetemplates API.
type GoofyMachineTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec GoofyMachineTemplateSpec `json:"spec,omitempty"`
}

// +kubebuilder:object:root=true

// GoofyMachineTemplateList contains a list of GoofyMachineTemplate.
type GoofyMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []GoofyMachineTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&GoofyMachineTemplate{}, &GoofyMachineTemplateList{})
}

// GoofyMachineTemplateResource describes the data needed to create a GoofyMachine from a template.
type GoofyMachineTemplateResource struct {
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitempty"`
	// Spec is the specification of the desired behavior of the machine.
	Spec GoofyMachineSpec `json:"spec"`
}
