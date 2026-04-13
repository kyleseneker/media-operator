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

// ProwlarrConfigSpec defines the desired state of ProwlarrConfig.
type ProwlarrConfigSpec struct {
	// connection defines how to connect to the Prowlarr instance.
	// +required
	Connection commonv1alpha1.AppConnection `json:"connection"`

	// reconcile configures reconciliation behavior (e.g., sync interval).
	// +optional
	Reconcile *commonv1alpha1.ReconcileConfig `json:"reconcile,omitempty"`

	// applications defines application connections managed by Prowlarr.
	// +optional
	Applications []ProwlarrApplication `json:"applications,omitempty"`

	// indexers defines indexer configurations managed by Prowlarr.
	// +optional
	Indexers []ProwlarrIndexer `json:"indexers,omitempty"`

	// proxies defines proxy configurations for Prowlarr.
	// +optional
	Proxies []ProwlarrProxy `json:"proxies,omitempty"`

	// tags defines tags for linking resources together.
	// +optional
	Tags []commonv1alpha1.Tag `json:"tags,omitempty"`

	// downloadClients defines download client connections for manual grabs.
	// +optional
	DownloadClients []ProwlarrDownloadClient `json:"downloadClients,omitempty"`

	// notifications defines notification/connection configurations.
	// +optional
	Notifications []commonv1alpha1.Notification `json:"notifications,omitempty"`
}

// ProwlarrApplication defines an application connection managed by Prowlarr.
type ProwlarrApplication struct {
	// name is the display name of the application.
	// +required
	Name string `json:"name"`

	// syncLevel controls how indexers are synced to this application.
	// +optional
	// +kubebuilder:validation:Enum=disabled;addOnly;fullSync
	SyncLevel string `json:"syncLevel,omitempty"`

	// implementation is the application type (e.g., Sonarr, Radarr).
	// +required
	Implementation string `json:"implementation"`

	// configContract is the configuration contract name for the implementation.
	// +required
	ConfigContract string `json:"configContract"`

	// prowlarrUrl is the URL Prowlarr uses to reach itself (for callbacks).
	// +optional
	ProwlarrUrl string `json:"prowlarrUrl,omitempty"`

	// baseUrl is the base URL of the application.
	// +required
	BaseUrl string `json:"baseUrl"`

	// apiKeySecretRef references a Secret containing the application's API key.
	// +required
	APIKeySecretRef commonv1alpha1.SecretKeyRef `json:"apiKeySecretRef"`

	// syncCategories is the list of category IDs to sync to this application.
	// +optional
	SyncCategories []int `json:"syncCategories,omitempty"`

	// tags is the list of tag IDs to associate with this application.
	// +optional
	Tags []int `json:"tags,omitempty"`
}

// ProwlarrIndexer defines an indexer configuration in Prowlarr.
type ProwlarrIndexer struct {
	// name is the display name of the indexer.
	// +required
	Name string `json:"name"`

	// implementation is the indexer type (e.g., Newznab, Torznab).
	// +required
	Implementation string `json:"implementation"`

	// configContract is the configuration contract name for the implementation.
	// +required
	ConfigContract string `json:"configContract"`

	// enable controls whether this indexer is active.
	// +optional
	// +kubebuilder:default=true
	Enable *bool `json:"enable,omitempty"`

	// appProfileId is the application profile ID for this indexer.
	// +optional
	AppProfileId *int `json:"appProfileId,omitempty"`

	// priority is the indexer priority. Lower values are higher priority.
	// +optional
	Priority *int `json:"priority,omitempty"`

	// fields defines configuration fields for the indexer.
	// +optional
	Fields []ProwlarrField `json:"fields,omitempty"`

	// tags is the list of tag IDs to associate with this indexer.
	// +optional
	Tags []int `json:"tags,omitempty"`
}

// ProwlarrField represents a key-value configuration field.
type ProwlarrField struct {
	// name is the field name.
	// +required
	Name string `json:"name"`

	// value is the field value. Use "ENV:VAR_NAME" convention for secret references.
	// +optional
	Value *string `json:"value,omitempty"`
}

// ProwlarrProxy defines a proxy configuration for Prowlarr.
type ProwlarrProxy struct {
	// name is the display name of the proxy.
	// +required
	Name string `json:"name"`

	// implementation is the proxy type (e.g., Http, Socks4, Socks5).
	// +required
	Implementation string `json:"implementation"`

	// configContract is the configuration contract name for the implementation.
	// +required
	ConfigContract string `json:"configContract"`

	// host is the hostname or IP of the proxy.
	// +required
	Host string `json:"host"`

	// requestTimeout is the request timeout in seconds.
	// +optional
	RequestTimeout *int `json:"requestTimeout,omitempty"`

	// tags is the list of tag IDs to associate with this proxy.
	// +optional
	Tags []int `json:"tags,omitempty"`
}

// ProwlarrDownloadClient defines a download client for Prowlarr manual grabs.
type ProwlarrDownloadClient struct {
	// name is the display name of the download client.
	// +required
	Name string `json:"name"`

	// enable controls whether this download client is active.
	// +optional
	// +kubebuilder:default=true
	Enable *bool `json:"enable,omitempty"`

	// protocol is the download protocol.
	// +required
	// +kubebuilder:validation:Enum=torrent;usenet
	Protocol string `json:"protocol"`

	// implementation is the download client type (e.g., QBittorrent, Sabnzbd).
	// +required
	Implementation string `json:"implementation"`

	// configContract is the configuration contract name.
	// Defaults to "<implementation>Settings" if not set.
	// +optional
	ConfigContract string `json:"configContract,omitempty"`

	// priority is the download client priority. Lower values are higher priority.
	// +optional
	Priority *int `json:"priority,omitempty"`

	// categories defines the category mappings for this download client.
	// +optional
	Categories []ProwlarrDownloadClientCategory `json:"categories,omitempty"`

	// tags is the list of tag IDs to associate with this download client.
	// +optional
	Tags []int `json:"tags,omitempty"`

	// fields defines the download client's configuration fields (e.g., host, port, apiKey).
	// +optional
	Fields []ProwlarrField `json:"fields,omitempty"`
}

// ProwlarrDownloadClientCategory maps Prowlarr categories to download client categories.
type ProwlarrDownloadClientCategory struct {
	// clientCategory is the category name in the download client.
	// +required
	ClientCategory string `json:"clientCategory"`

	// categories is the list of Prowlarr category IDs to map.
	// +optional
	Categories []int `json:"categories,omitempty"`
}

// ProwlarrConfigStatus defines the observed state of ProwlarrConfig.
type ProwlarrConfigStatus struct {
	// conditions represent the current state of the ProwlarrConfig resource.
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

// ProwlarrConfig is the Schema for the prowlarrconfigs API.
type ProwlarrConfig struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec ProwlarrConfigSpec `json:"spec"`

	// +optional
	Status ProwlarrConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ProwlarrConfigList contains a list of ProwlarrConfig.
type ProwlarrConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ProwlarrConfig `json:"items"`
}

func (c *ProwlarrConfig) GetConditions() *[]metav1.Condition { return &c.Status.Conditions }
func (c *ProwlarrConfig) GetObservedGeneration() *int64      { return &c.Status.ObservedGeneration }
func (c *ProwlarrConfig) GetLastSyncTime() **metav1.Time     { return &c.Status.LastSyncTime }
func (c *ProwlarrConfig) GetReconcileConfig() *commonv1alpha1.ReconcileConfig {
	return c.Spec.Reconcile
}

func init() {
	SchemeBuilder.Register(&ProwlarrConfig{}, &ProwlarrConfigList{})
}
