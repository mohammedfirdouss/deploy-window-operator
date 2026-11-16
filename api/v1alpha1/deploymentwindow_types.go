/*
Copyright 2026.

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
	"k8s.io/apimachinery/pkg/runtime"
)

// Window is one period, on the given days, when rollouts are allowed.
type Window struct {
	// Days the window applies to. Three-letter English names.
	// +kubebuilder:validation:MinItems=1
	// +kubebuilder:validation:items:Enum=Mon;Tue;Wed;Thu;Fri;Sat;Sun
	Days []string `json:"days"`

	// Start time in 24-hour HH:MM, in spec.timeZone.
	// +kubebuilder:validation:Pattern=`^([01][0-9]|2[0-3]):[0-5][0-9]$`
	Start string `json:"start"`

	// End time in 24-hour HH:MM, in spec.timeZone. Must be after start;
	// windows do not cross midnight (use two windows instead).
	// +kubebuilder:validation:Pattern=`^([01][0-9]|2[0-3]):[0-5][0-9]$`
	End string `json:"end"`
}

// DeploymentWindowSpec defines when the selected Deployments may roll out.
type DeploymentWindowSpec struct {
	// Selector picks Deployments in the same namespace as this object.
	Selector metav1.LabelSelector `json:"selector"`

	// TimeZone is an IANA name such as "Africa/Lagos". Defaults to UTC.
	// +kubebuilder:default=UTC
	// +optional
	TimeZone string `json:"timeZone,omitempty"`

	// Windows when rollouts are allowed. Outside all windows, the selected
	// Deployments are paused (spec.paused=true) so new rollouts wait.
	// +kubebuilder:validation:MinItems=1
	Windows []Window `json:"windows"`

	// Suspend stops the controller from pausing anything and resumes any
	// Deployment it paused. Useful for an emergency fix outside a window.
	// +optional
	Suspend bool `json:"suspend,omitempty"`
}

// DeploymentWindowStatus is the observed state of a DeploymentWindow.
type DeploymentWindowStatus struct {
	// Open is true when rollouts are currently allowed.
	// +optional
	Open bool `json:"open"`

	// NextTransition is when Open will next change.
	// +optional
	NextTransition *metav1.Time `json:"nextTransition,omitempty"`

	// MatchedDeployments is how many Deployments the selector matches.
	// +optional
	MatchedDeployments int32 `json:"matchedDeployments"`

	// PausedDeployments lists the Deployments this window has paused.
	// +optional
	PausedDeployments []string `json:"pausedDeployments,omitempty"`

	// ObservedGeneration is the spec generation this status reflects.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions: "Ready" reports whether the spec is valid and was applied.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=dw
// +kubebuilder:printcolumn:name="Open",type=boolean,JSONPath=`.status.open`
// +kubebuilder:printcolumn:name="Matched",type=integer,JSONPath=`.status.matchedDeployments`
// +kubebuilder:printcolumn:name="Next",type=date,JSONPath=`.status.nextTransition`
// +kubebuilder:printcolumn:name="Suspended",type=boolean,JSONPath=`.spec.suspend`

// DeploymentWindow pauses selected Deployments outside allowed time windows.
type DeploymentWindow struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DeploymentWindowSpec   `json:"spec"`
	Status DeploymentWindowStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DeploymentWindowList contains a list of DeploymentWindow.
type DeploymentWindowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DeploymentWindow `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &DeploymentWindow{}, &DeploymentWindowList{})
		return nil
	})
}
