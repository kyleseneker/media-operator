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

// MaintainerrConfigSpec defines the desired state of MaintainerrConfig.
type MaintainerrConfigSpec struct {
	// connection defines how to connect to the Maintainerr instance.
	// Maintainerr uses API key authentication.
	// +required
	Connection commonv1alpha1.AppConnection `json:"connection"`

	// reconcile configures reconciliation behavior (e.g., sync interval).
	// +optional
	Reconcile *commonv1alpha1.ReconcileConfig `json:"reconcile,omitempty"`

	// plexConnection configures the Plex server connection within Maintainerr.
	// +optional
	PlexConnection *MaintainerrPlexConnection `json:"plexConnection,omitempty"`

	// sonarrConnection configures the Sonarr connection within Maintainerr.
	// +optional
	SonarrConnection *MaintainerrArrConnection `json:"sonarrConnection,omitempty"`

	// radarrConnection configures the Radarr connection within Maintainerr.
	// +optional
	RadarrConnection *MaintainerrArrConnection `json:"radarrConnection,omitempty"`

	// overseerrConnection configures the Overseerr/Jellyseerr connection within Maintainerr.
	// +optional
	OverseerrConnection *MaintainerrArrConnection `json:"overseerrConnection,omitempty"`

	// rules defines media management rules for automatic collection cleanup.
	// +optional
	Rules []MaintainerrRule `json:"rules,omitempty"`

	// settings configures general Maintainerr settings.
	// +optional
	Settings *MaintainerrSettings `json:"settings,omitempty"`
}

// MaintainerrPlexConnection configures the Plex connection in Maintainerr.
type MaintainerrPlexConnection struct {
	// url is the Plex server URL (e.g., http://plex:32400).
	// +required
	URL string `json:"url"`

	// tokenSecretRef references a Secret containing the Plex authentication token.
	// +required
	TokenSecretRef commonv1alpha1.SecretKeyRef `json:"tokenSecretRef"`
}

// MaintainerrArrConnection configures a *arr or Overseerr connection in Maintainerr.
type MaintainerrArrConnection struct {
	// url is the app URL (e.g., http://sonarr:8989).
	// +required
	URL string `json:"url"`

	// apiKeySecretRef references a Secret containing the app's API key.
	// +required
	APIKeySecretRef commonv1alpha1.SecretKeyRef `json:"apiKeySecretRef"`
}

// MaintainerrRule defines a media management rule.
type MaintainerrRule struct {
	// name is the display name of the rule.
	// +required
	Name string `json:"name"`

	// enable controls whether this rule is active.
	// +optional
	// +kubebuilder:default=true
	Enable *bool `json:"enable,omitempty"`

	// libraryName is the Plex library this rule applies to (e.g., "Movies", "TV Shows").
	// +required
	LibraryName string `json:"libraryName"`

	// mediaType is the type of media this rule targets.
	// +required
	// +kubebuilder:validation:Enum=movie;show
	MediaType string `json:"mediaType"`

	// action defines what happens to matched media.
	// +required
	// +kubebuilder:validation:Enum=delete;unmonitor
	Action string `json:"action"`

	// deleteFromDisk also deletes the media files from disk (only when action=delete).
	// +optional
	DeleteFromDisk *bool `json:"deleteFromDisk,omitempty"`

	// conditions defines the criteria for matching media.
	// All conditions must be met for media to be matched (AND logic).
	// +optional
	Conditions []MaintainerrRuleCondition `json:"conditions,omitempty"`
}

// MaintainerrRuleCondition defines a condition for a media management rule.
type MaintainerrRuleCondition struct {
	// field is the media field to evaluate.
	// +required
	// +kubebuilder:validation:Enum=addedAt;lastViewedAt;viewCount;rating;year;fileSize;resolution;genre;label
	Field string `json:"field"`

	// operator is the comparison operator.
	// +required
	// +kubebuilder:validation:Enum=equals;not_equals;contains;not_contains;greater_than;less_than;before;after;in_last;not_in_last
	Operator string `json:"operator"`

	// value is the value to compare against. For date fields, use duration format
	// (e.g., "30d" for 30 days, "6m" for 6 months). For numeric fields, use plain numbers.
	// For string fields, use the literal value.
	// +required
	Value string `json:"value"`
}

// MaintainerrSettings configures general Maintainerr behavior.
type MaintainerrSettings struct {
	// collectionHandling controls how Maintainerr manages Plex collections.
	// +optional
	// +kubebuilder:validation:Enum=keep;delete
	CollectionHandling string `json:"collectionHandling,omitempty"`

	// dryRun prevents actual deletions and only logs what would happen.
	// +optional
	DryRun *bool `json:"dryRun,omitempty"`
}

// MaintainerrConfigStatus defines the observed state of MaintainerrConfig.
type MaintainerrConfigStatus struct {
	// conditions represent the current state of the MaintainerrConfig resource.
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
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=`.status.conditions[?(@.type=="Synced")].status`
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Last Sync",type="date",JSONPath=`.status.lastSyncTime`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`

// MaintainerrConfig is the Schema for the maintainerrconfigs API.
type MaintainerrConfig struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec MaintainerrConfigSpec `json:"spec"`

	// +optional
	Status MaintainerrConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// MaintainerrConfigList contains a list of MaintainerrConfig.
type MaintainerrConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []MaintainerrConfig `json:"items"`
}

func (c *MaintainerrConfig) GetConditions() *[]metav1.Condition { return &c.Status.Conditions }
func (c *MaintainerrConfig) GetObservedGeneration() *int64      { return &c.Status.ObservedGeneration }
func (c *MaintainerrConfig) GetLastSyncTime() **metav1.Time     { return &c.Status.LastSyncTime }
func (c *MaintainerrConfig) GetReconcileConfig() *commonv1alpha1.ReconcileConfig {
	return c.Spec.Reconcile
}

func init() {
	SchemeBuilder.Register(&MaintainerrConfig{}, &MaintainerrConfigList{})
}
