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

// QBittorrentConfigSpec defines the desired state of QBittorrentConfig.
type QBittorrentConfigSpec struct {
	// connection defines how to connect to the qBittorrent instance.
	// +required
	Connection QBittorrentConnection `json:"connection"`

	// reconcile configures reconciliation behavior (e.g., sync interval).
	// +optional
	Reconcile *commonv1alpha1.ReconcileConfig `json:"reconcile,omitempty"`

	// preferences configures qBittorrent application preferences.
	// +optional
	Preferences *QBittorrentPreferences `json:"preferences,omitempty"`

	// categories defines download categories and their save paths.
	// +optional
	Categories []QBittorrentCategory `json:"categories,omitempty"`
}

// QBittorrentConnection defines how the operator connects to qBittorrent.
// Unlike AppConnection, qBittorrent uses username/password authentication.
type QBittorrentConnection struct {
	// url is the base URL of the qBittorrent Web UI (e.g., http://qbittorrent.media.svc.cluster.local:8080).
	// +required
	URL string `json:"url"`

	// usernameSecretRef references a Secret containing the username.
	// +required
	UsernameSecretRef commonv1alpha1.SecretKeyRef `json:"usernameSecretRef"`

	// passwordSecretRef references a Secret containing the password.
	// +required
	PasswordSecretRef commonv1alpha1.SecretKeyRef `json:"passwordSecretRef"`

	// tls configures TLS settings for the connection.
	// Only needed when qBittorrent uses HTTPS with self-signed or private CA certificates.
	// +optional
	TLS *commonv1alpha1.TLSConfig `json:"tls,omitempty"`
}

// QBittorrentPreferences configures qBittorrent application preferences.
type QBittorrentPreferences struct {
	// savePath is the default save path for downloads.
	// +optional
	SavePath string `json:"savePath,omitempty"`

	// tempPathEnabled enables using a temporary path for incomplete downloads.
	// +optional
	TempPathEnabled *bool `json:"tempPathEnabled,omitempty"`

	// dht enables the DHT (Distributed Hash Table) network.
	// +optional
	DHT *bool `json:"dht,omitempty"`

	// pex enables Peer Exchange.
	// +optional
	PEX *bool `json:"pex,omitempty"`

	// lsd enables Local Service Discovery.
	// +optional
	LSD *bool `json:"lsd,omitempty"`

	// encryption controls the encryption mode. 0=prefer, 1=force on, 2=force off.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=2
	Encryption *int `json:"encryption,omitempty"`

	// maxConnec is the global maximum number of connections. -1 means unlimited.
	// +optional
	MaxConnec *int `json:"maxConnec,omitempty"`

	// maxConnecPerTorrent is the maximum connections per torrent. -1 means unlimited.
	// +optional
	MaxConnecPerTorrent *int `json:"maxConnecPerTorrent,omitempty"`

	// maxUploads is the global maximum number of upload slots. -1 means unlimited.
	// +optional
	MaxUploads *int `json:"maxUploads,omitempty"`

	// maxUploadsPerTorrent is the maximum upload slots per torrent. -1 means unlimited.
	// +optional
	MaxUploadsPerTorrent *int `json:"maxUploadsPerTorrent,omitempty"`

	// maxRatioEnabled enables the global share ratio limit.
	// +optional
	MaxRatioEnabled *bool `json:"maxRatioEnabled,omitempty"`

	// maxRatio is the global share ratio limit (e.g., "2.0").
	// Represented as a string to preserve decimal precision in JSON.
	// +optional
	MaxRatio *string `json:"maxRatio,omitempty"`

	// maxSeedingTimeEnabled enables the global seeding time limit.
	// +optional
	MaxSeedingTimeEnabled *bool `json:"maxSeedingTimeEnabled,omitempty"`

	// maxSeedingTime is the global seeding time limit in minutes.
	// +optional
	MaxSeedingTime *int `json:"maxSeedingTime,omitempty"`

	// preallocateAll enables disk space pre-allocation for all files.
	// +optional
	PreallocateAll *bool `json:"preallocateAll,omitempty"`

	// locale is the UI locale (e.g., "en").
	// +optional
	Locale string `json:"locale,omitempty"`

	// webUiPort is the Web UI listening port.
	// +optional
	WebUIPort *int `json:"webUiPort,omitempty"`
}

// QBittorrentCategory defines a download category with its save path.
type QBittorrentCategory struct {
	// name is the category name.
	// +required
	Name string `json:"name"`

	// savePath is the save path for this category.
	// +optional
	SavePath string `json:"savePath,omitempty"`
}

// QBittorrentConfigStatus defines the observed state of QBittorrentConfig.
type QBittorrentConfigStatus struct {
	// conditions represent the current state of the QBittorrentConfig resource.
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

// QBittorrentConfig is the Schema for the qbittorrentconfigs API.
type QBittorrentConfig struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec QBittorrentConfigSpec `json:"spec"`

	// +optional
	Status QBittorrentConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// QBittorrentConfigList contains a list of QBittorrentConfig.
type QBittorrentConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []QBittorrentConfig `json:"items"`
}

func (c *QBittorrentConfig) GetConditions() *[]metav1.Condition { return &c.Status.Conditions }
func (c *QBittorrentConfig) GetObservedGeneration() *int64      { return &c.Status.ObservedGeneration }
func (c *QBittorrentConfig) GetLastSyncTime() **metav1.Time     { return &c.Status.LastSyncTime }
func (c *QBittorrentConfig) GetReconcileConfig() *commonv1alpha1.ReconcileConfig {
	return c.Spec.Reconcile
}

func init() {
	SchemeBuilder.Register(&QBittorrentConfig{}, &QBittorrentConfigList{})
}
