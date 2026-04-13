package v1alpha1

import (
	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SabnzbdConfigSpec defines the desired state of SabnzbdConfig.
type SabnzbdConfigSpec struct {
	// connection defines how to connect to the SABnzbd instance.
	// +required
	Connection commonv1alpha1.AppConnection `json:"connection"`

	// reconcile configures reconciliation behavior (e.g., sync interval).
	// +optional
	Reconcile *commonv1alpha1.ReconcileConfig `json:"reconcile,omitempty"`

	// general configures general SABnzbd settings.
	// +optional
	General *SabnzbdGeneral `json:"general,omitempty"`

	// servers defines Usenet server connections.
	// +optional
	Servers []SabnzbdServer `json:"servers,omitempty"`

	// categories defines download categories.
	// +optional
	Categories []SabnzbdCategory `json:"categories,omitempty"`

	// folders configures download and temp folder paths.
	// +optional
	Folders *SabnzbdFolders `json:"folders,omitempty"`
}

// SabnzbdGeneral configures general SABnzbd settings.
type SabnzbdGeneral struct {
	// downloadSpeedLimit is the download speed limit (e.g., "100M", "0" for unlimited).
	// +optional
	DownloadSpeedLimit string `json:"downloadSpeedLimit,omitempty"`

	// pauseOnPostProcessing pauses downloading during post-processing.
	// +optional
	PauseOnPostProcessing *bool `json:"pauseOnPostProcessing,omitempty"`

	// scriptDir is the path to the scripts directory.
	// +optional
	ScriptDir string `json:"scriptDir,omitempty"`

	// autoSort enables automatic sorting of downloads.
	// +optional
	AutoSort *bool `json:"autoSort,omitempty"`

	// preCheck enables NZB pre-check for completion.
	// +optional
	PreCheck *bool `json:"preCheck,omitempty"`
}

// SabnzbdServer defines a Usenet server connection.
type SabnzbdServer struct {
	// name is the display name of the server.
	// +required
	Name string `json:"name"`

	// host is the hostname of the Usenet server.
	// +required
	Host string `json:"host"`

	// port is the port number.
	// +required
	Port int `json:"port"`

	// ssl enables SSL/TLS connection.
	// +optional
	SSL *bool `json:"ssl,omitempty"`

	// usernameSecretRef references a Secret containing the username.
	// +optional
	UsernameSecretRef *commonv1alpha1.SecretKeyRef `json:"usernameSecretRef,omitempty"`

	// passwordSecretRef references a Secret containing the password.
	// +optional
	PasswordSecretRef *commonv1alpha1.SecretKeyRef `json:"passwordSecretRef,omitempty"`

	// connections is the number of simultaneous connections.
	// +optional
	Connections *int `json:"connections,omitempty"`

	// priority is the server priority. 0 = highest.
	// +optional
	Priority *int `json:"priority,omitempty"`

	// retention is the server retention in days. 0 = unlimited.
	// +optional
	Retention *int `json:"retention,omitempty"`

	// enable controls whether this server is active.
	// +optional
	// +kubebuilder:default=true
	Enable *bool `json:"enable,omitempty"`
}

// SabnzbdCategory defines a download category.
type SabnzbdCategory struct {
	// name is the category name.
	// +required
	Name string `json:"name"`

	// dir is the subdirectory for this category (relative to complete dir).
	// +optional
	Dir string `json:"dir,omitempty"`

	// script is the post-processing script for this category.
	// +optional
	Script string `json:"script,omitempty"`

	// priority is the download priority. -100=default, -2=paused, -1=low, 0=normal, 1=high, 2=force.
	// +optional
	Priority *int `json:"priority,omitempty"`
}

// SabnzbdFolders configures folder paths.
type SabnzbdFolders struct {
	// completeDir is the path for completed downloads.
	// +optional
	CompleteDir string `json:"completeDir,omitempty"`

	// incompleteDir is the path for incomplete downloads.
	// +optional
	IncompleteDir string `json:"incompleteDir,omitempty"`

	// tempDownloadDir is the temporary download path.
	// +optional
	TempDownloadDir string `json:"tempDownloadDir,omitempty"`

	// nzbBackupDir is the path for NZB backups.
	// +optional
	NzbBackupDir string `json:"nzbBackupDir,omitempty"`

	// scriptDir is the path for scripts.
	// +optional
	ScriptDir string `json:"scriptDir,omitempty"`
}

// SabnzbdConfigStatus defines the observed state of SabnzbdConfig.
type SabnzbdConfigStatus struct {
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	LastSyncTime       *metav1.Time       `json:"lastSyncTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=`.status.conditions[?(@.type=="Synced")].status`
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Last Sync",type="date",JSONPath=`.status.lastSyncTime`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`

// SabnzbdConfig is the Schema for the sabnzbdconfigs API.
type SabnzbdConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`
	Spec              SabnzbdConfigSpec   `json:"spec"`
	Status            SabnzbdConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SabnzbdConfigList contains a list of SabnzbdConfig.
type SabnzbdConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SabnzbdConfig `json:"items"`
}

func (c *SabnzbdConfig) GetConditions() *[]metav1.Condition                  { return &c.Status.Conditions }
func (c *SabnzbdConfig) GetObservedGeneration() *int64                       { return &c.Status.ObservedGeneration }
func (c *SabnzbdConfig) GetLastSyncTime() **metav1.Time                      { return &c.Status.LastSyncTime }
func (c *SabnzbdConfig) GetReconcileConfig() *commonv1alpha1.ReconcileConfig { return c.Spec.Reconcile }

func init() {
	SchemeBuilder.Register(&SabnzbdConfig{}, &SabnzbdConfigList{})
}
