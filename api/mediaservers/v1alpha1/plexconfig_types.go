package v1alpha1

import (
	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PlexConfigSpec defines the desired state of PlexConfig.
type PlexConfigSpec struct {
	// connection defines how to connect to the Plex instance.
	// +required
	Connection PlexConnection `json:"connection"`

	// reconcile configures reconciliation behavior (e.g., sync interval).
	// +optional
	Reconcile *commonv1alpha1.ReconcileConfig `json:"reconcile,omitempty"`

	// server configures Plex server settings.
	// +optional
	Server *PlexServer `json:"server,omitempty"`

	// transcoder configures transcoding and hardware acceleration.
	// +optional
	Transcoder *PlexTranscoder `json:"transcoder,omitempty"`

	// libraries defines media libraries to create.
	// +optional
	Libraries []PlexLibrary `json:"libraries,omitempty"`

	// network configures network settings.
	// +optional
	Network *PlexNetwork `json:"network,omitempty"`
}

// PlexConnection defines how the operator connects to Plex.
type PlexConnection struct {
	// url is the base URL of the Plex server (e.g., http://plex:32400).
	// +required
	URL string `json:"url"`

	// tokenSecretRef references a Secret containing the X-Plex-Token.
	// +required
	TokenSecretRef commonv1alpha1.SecretKeyRef `json:"tokenSecretRef"`

	// tls configures TLS settings for the connection.
	// Only needed when Plex uses HTTPS with self-signed or private CA certificates.
	// +optional
	TLS *commonv1alpha1.TLSConfig `json:"tls,omitempty"`
}

// PlexServer configures Plex server settings.
type PlexServer struct {
	// friendlyName is the server's display name.
	// +optional
	FriendlyName string `json:"friendlyName,omitempty"`

	// language is the preferred language (ISO 639-1, e.g., "en").
	// +optional
	Language string `json:"language,omitempty"`

	// enableRemoteAccess allows access from outside the local network.
	// +optional
	EnableRemoteAccess *bool `json:"enableRemoteAccess,omitempty"`

	// logDebug enables debug logging.
	// +optional
	LogDebug *bool `json:"logDebug,omitempty"`

	// autoEmptyTrash automatically empties the trash.
	// +optional
	AutoEmptyTrash *bool `json:"autoEmptyTrash,omitempty"`

	// scanMyLibraryAutomatically enables automatic library scanning.
	// +optional
	ScanMyLibraryAutomatically *bool `json:"scanMyLibraryAutomatically,omitempty"`

	// scanMyLibraryPeriodically enables periodic library scanning.
	// +optional
	ScanMyLibraryPeriodically *bool `json:"scanMyLibraryPeriodically,omitempty"`
}

// PlexTranscoder configures transcoding settings.
type PlexTranscoder struct {
	// transcodeHwRequested enables hardware-accelerated transcoding.
	// +optional
	TranscodeHwRequested *bool `json:"transcodeHwRequested,omitempty"`

	// hardwareAccelerationType sets the HW acceleration type.
	// +optional
	HardwareAccelerationType string `json:"hardwareAccelerationType,omitempty"`

	// maxSimultaneousVideoTranscodes limits concurrent transcodes. 0 = unlimited.
	// +optional
	MaxSimultaneousVideoTranscodes *int `json:"maxSimultaneousVideoTranscodes,omitempty"`

	// transcodeHwDecodingEnabled enables hardware decoding.
	// +optional
	TranscodeHwDecodingEnabled *bool `json:"transcodeHwDecodingEnabled,omitempty"`

	// transcodeHwEncodingEnabled enables hardware encoding.
	// +optional
	TranscodeHwEncodingEnabled *bool `json:"transcodeHwEncodingEnabled,omitempty"`

	// transcoderTempDirectory sets the transcoder temporary directory.
	// +optional
	TranscoderTempDirectory string `json:"transcoderTempDirectory,omitempty"`
}

// PlexLibrary defines a Plex media library.
type PlexLibrary struct {
	// name is the display name of the library.
	// +required
	Name string `json:"name"`

	// type is the library type.
	// +required
	// +kubebuilder:validation:Enum=movie;show;artist;photo
	Type string `json:"type"`

	// paths is the list of media folder paths.
	// +required
	Paths []string `json:"paths"`

	// language is the library language (ISO 639-1).
	// +optional
	Language string `json:"language,omitempty"`

	// scanner is the scanner type.
	// +optional
	Scanner string `json:"scanner,omitempty"`

	// agent is the metadata agent to use.
	// +optional
	Agent string `json:"agent,omitempty"`
}

// PlexNetwork configures Plex network settings.
type PlexNetwork struct {
	// secureConnections controls when secure connections are required.
	// +optional
	// +kubebuilder:validation:Enum=0;1;2
	SecureConnections *int `json:"secureConnections,omitempty"`

	// customServerAccessUrls is a comma-separated list of custom access URLs.
	// +optional
	CustomServerAccessUrls string `json:"customServerAccessUrls,omitempty"`

	// allowedNetworks is a comma-separated list of networks allowed without auth.
	// +optional
	AllowedNetworks string `json:"allowedNetworks,omitempty"`

	// enableIPv6 enables IPv6 support.
	// +optional
	EnableIPv6 *bool `json:"enableIPv6,omitempty"`
}

// PlexConfigStatus defines the observed state of PlexConfig.
type PlexConfigStatus struct {
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

// PlexConfig is the Schema for the plexconfigs API.
type PlexConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`
	Spec              PlexConfigSpec   `json:"spec"`
	Status            PlexConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// PlexConfigList contains a list of PlexConfig.
type PlexConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []PlexConfig `json:"items"`
}

func (c *PlexConfig) GetConditions() *[]metav1.Condition                  { return &c.Status.Conditions }
func (c *PlexConfig) GetObservedGeneration() *int64                       { return &c.Status.ObservedGeneration }
func (c *PlexConfig) GetLastSyncTime() **metav1.Time                      { return &c.Status.LastSyncTime }
func (c *PlexConfig) GetReconcileConfig() *commonv1alpha1.ReconcileConfig { return c.Spec.Reconcile }

func init() {
	SchemeBuilder.Register(&PlexConfig{}, &PlexConfigList{})
}
