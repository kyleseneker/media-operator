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
	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// FlareSolverrConfigSpec defines the desired state of FlareSolverrConfig.
type FlareSolverrConfigSpec struct {
	// connection defines how to connect to the FlareSolverr instance.
	// FlareSolverr does not require authentication by default.
	// +required
	Connection FlareSolverrConnection `json:"connection"`

	// reconcile configures reconciliation behavior (e.g., sync interval).
	// +optional
	Reconcile *commonv1alpha1.ReconcileConfig `json:"reconcile,omitempty"`

	// sessions defines persistent browser sessions to maintain.
	// Named sessions are reused across requests and survive between solves,
	// improving performance for repeated requests to the same site.
	// +optional
	Sessions []FlareSolverrSession `json:"sessions,omitempty"`
}

// FlareSolverrConnection defines how the operator connects to a FlareSolverr instance.
// FlareSolverr typically has no authentication; it is expected to be network-isolated.
type FlareSolverrConnection struct {
	// url is the base URL of the FlareSolverr instance (e.g., http://flaresolverr:8191).
	// +required
	URL string `json:"url"`

	// tls configures TLS settings for the connection.
	// Only needed when FlareSolverr uses HTTPS with self-signed or private CA certificates.
	// +optional
	TLS *commonv1alpha1.TLSConfig `json:"tls,omitempty"`
}

// FlareSolverrSession defines a named browser session to maintain in FlareSolverr.
type FlareSolverrSession struct {
	// name is the session identifier. Must be unique across sessions.
	// This name is used when making requests through Prowlarr or other tools
	// to reuse the same browser session.
	// +required
	Name string `json:"name"`
}

// FlareSolverrConfigStatus defines the observed state of FlareSolverrConfig.
type FlareSolverrConfigStatus struct {
	// conditions represent the current state of the FlareSolverrConfig resource.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the most recent generation observed.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// lastSyncTime is the timestamp of the last successful reconciliation.
	// +optional
	LastSyncTime *metav1.Time `json:"lastSyncTime,omitempty"`

	// activeSessions is the number of active sessions managed by this resource.
	// +optional
	ActiveSessions int `json:"activeSessions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=`.status.conditions[?(@.type=="Synced")].status`
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Sessions",type="integer",JSONPath=`.status.activeSessions`
// +kubebuilder:printcolumn:name="Last Sync",type="date",JSONPath=`.status.lastSyncTime`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`

// FlareSolverrConfig is the Schema for the flaresolverrconfigs API.
type FlareSolverrConfig struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec FlareSolverrConfigSpec `json:"spec"`

	// +optional
	Status FlareSolverrConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// FlareSolverrConfigList contains a list of FlareSolverrConfig.
type FlareSolverrConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []FlareSolverrConfig `json:"items"`
}

func (c *FlareSolverrConfig) GetConditions() *[]metav1.Condition { return &c.Status.Conditions }
func (c *FlareSolverrConfig) GetObservedGeneration() *int64      { return &c.Status.ObservedGeneration }
func (c *FlareSolverrConfig) GetLastSyncTime() **metav1.Time     { return &c.Status.LastSyncTime }
func (c *FlareSolverrConfig) GetReconcileConfig() *commonv1alpha1.ReconcileConfig {
	return c.Spec.Reconcile
}

func init() {
	SchemeBuilder.Register(&FlareSolverrConfig{}, &FlareSolverrConfigList{})
}
