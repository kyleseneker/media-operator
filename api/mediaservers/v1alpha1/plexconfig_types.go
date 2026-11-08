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

	// language is not a setting current Plex has; Plex rejects it and reports why.
	// Set a library's language on the library instead.
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

	// scanMyLibraryPartially scans only the folders that changed when changes
	// are detected.
	// +optional
	ScanMyLibraryPartially *bool `json:"scanMyLibraryPartially,omitempty"`

	// libraryScanInterval is how often, in seconds, scanMyLibraryPeriodically scans.
	// +optional
	// +kubebuilder:validation:Enum=900;1800;3600;7200;21600;43200;86400
	LibraryScanInterval *int `json:"libraryScanInterval,omitempty"`

	// scannerLowPriority runs scans at a lower priority.
	// +optional
	ScannerLowPriority *bool `json:"scannerLowPriority,omitempty"`

	// allowMediaDeletion lets the server owner delete media from Plex apps.
	// +optional
	AllowMediaDeletion *bool `json:"allowMediaDeletion,omitempty"`

	// videoPlayedThreshold is how far, in percent, a video must play to count as watched.
	// +optional
	// +kubebuilder:validation:Enum=70;75;80;85;90;95
	VideoPlayedThreshold *int `json:"videoPlayedThreshold,omitempty"`

	// generateVideoPreviewThumbnails is when Plex makes the thumbnails shown
	// while seeking: never, as a scheduled task, or also as media is added.
	// +optional
	// +kubebuilder:validation:Enum=never;scheduled;asap
	GenerateVideoPreviewThumbnails string `json:"generateVideoPreviewThumbnails,omitempty"`

	// generateChapterThumbnails is when Plex makes chapter thumbnails.
	// +optional
	// +kubebuilder:validation:Enum=never;scheduled;asap
	GenerateChapterThumbnails string `json:"generateChapterThumbnails,omitempty"`

	// loudnessAnalysis is when Plex analyzes audio loudness.
	// +optional
	// +kubebuilder:validation:Enum=never;scheduled;asap
	LoudnessAnalysis string `json:"loudnessAnalysis,omitempty"`

	// butlerStartHour is the hour scheduled maintenance tasks may start.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=23
	ButlerStartHour *int `json:"butlerStartHour,omitempty"`

	// butlerEndHour is the hour scheduled maintenance tasks must stop.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=23
	ButlerEndHour *int `json:"butlerEndHour,omitempty"`

	// butlerTaskBackupDatabase backs up the database every three days.
	// +optional
	ButlerTaskBackupDatabase *bool `json:"butlerTaskBackupDatabase,omitempty"`

	// butlerDatabaseBackupPath is where database backups go.
	// +optional
	ButlerDatabaseBackupPath string `json:"butlerDatabaseBackupPath,omitempty"`

	// butlerTaskOptimizeDatabase optimizes the database every week.
	// +optional
	ButlerTaskOptimizeDatabase *bool `json:"butlerTaskOptimizeDatabase,omitempty"`

	// butlerTaskCleanOldBundles removes old metadata bundles every week.
	// +optional
	ButlerTaskCleanOldBundles *bool `json:"butlerTaskCleanOldBundles,omitempty"`

	// butlerTaskCleanOldCacheFiles removes old cache files every week.
	// +optional
	ButlerTaskCleanOldCacheFiles *bool `json:"butlerTaskCleanOldCacheFiles,omitempty"`

	// butlerTaskRefreshLocalMedia refreshes local metadata every three days.
	// +optional
	ButlerTaskRefreshLocalMedia *bool `json:"butlerTaskRefreshLocalMedia,omitempty"`

	// butlerTaskRefreshLibraries scans the libraries during maintenance.
	// +optional
	ButlerTaskRefreshLibraries *bool `json:"butlerTaskRefreshLibraries,omitempty"`

	// butlerTaskUpgradeMediaAnalysis upgrades media analysis during maintenance.
	// +optional
	ButlerTaskUpgradeMediaAnalysis *bool `json:"butlerTaskUpgradeMediaAnalysis,omitempty"`

	// butlerTaskRefreshPeriodicMetadata refreshes metadata periodically.
	// +optional
	ButlerTaskRefreshPeriodicMetadata *bool `json:"butlerTaskRefreshPeriodicMetadata,omitempty"`

	// butlerTaskDeepMediaAnalysis analyzes media in depth during maintenance.
	// +optional
	ButlerTaskDeepMediaAnalysis *bool `json:"butlerTaskDeepMediaAnalysis,omitempty"`

	// dlnaEnabled runs the DLNA server.
	// +optional
	DlnaEnabled *bool `json:"dlnaEnabled,omitempty"`

	// sendCrashReports sends crash reports to Plex.
	// +optional
	SendCrashReports *bool `json:"sendCrashReports,omitempty"`

	// logVerbose enables verbose logging.
	// +optional
	LogVerbose *bool `json:"logVerbose,omitempty"`
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

	// transcodeHwDecodingEnabled is not a setting current Plex has; Plex rejects it
	// and reports why. transcodeHwRequested covers hardware decoding.
	// +optional
	TranscodeHwDecodingEnabled *bool `json:"transcodeHwDecodingEnabled,omitempty"`

	// transcodeHwEncodingEnabled enables hardware encoding.
	// +optional
	TranscodeHwEncodingEnabled *bool `json:"transcodeHwEncodingEnabled,omitempty"`

	// transcoderTempDirectory sets the transcoder temporary directory.
	// +optional
	TranscoderTempDirectory string `json:"transcoderTempDirectory,omitempty"`

	// transcoderQuality trades speed for quality: 0 automatic, 1 prefer speed,
	// 2 prefer quality, 3 as slow and good as possible.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=3
	TranscoderQuality *int `json:"transcoderQuality,omitempty"`

	// transcoderThrottleBuffer is how many seconds ahead of playback the
	// transcoder works before slowing down.
	// +optional
	// +kubebuilder:validation:Minimum=0
	TranscoderThrottleBuffer *int `json:"transcoderThrottleBuffer,omitempty"`

	// transcoderH264BackgroundPreset is the encoder preset for background
	// transcodes such as optimizing and downloads.
	// +optional
	// +kubebuilder:validation:Enum=ultrafast;superfast;veryfast;faster;fast;medium;slow;slower;veryslow
	TranscoderH264BackgroundPreset string `json:"transcoderH264BackgroundPreset,omitempty"`

	// transcoderHevcEncoding allows HEVC encoding.
	// +optional
	TranscoderHevcEncoding *bool `json:"transcoderHevcEncoding,omitempty"`

	// downloadsTempDirectory is where transcodes for downloads are kept.
	// +optional
	DownloadsTempDirectory string `json:"downloadsTempDirectory,omitempty"`
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

	// manualPortMappingMode uses manualPortMappingPort as the public port for
	// remote access instead of finding one.
	// +optional
	ManualPortMappingMode *bool `json:"manualPortMappingMode,omitempty"`

	// manualPortMappingPort is the public port for remote access.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	ManualPortMappingPort *int `json:"manualPortMappingPort,omitempty"`

	// relayEnabled lets clients that can't connect directly go through Plex's relay.
	// +optional
	RelayEnabled *bool `json:"relayEnabled,omitempty"`

	// gdmEnabled announces the server to clients on the local network.
	// +optional
	GdmEnabled *bool `json:"gdmEnabled,omitempty"`

	// webhooksEnabled sends events to the webhooks set up in the Plex account.
	// +optional
	WebhooksEnabled *bool `json:"webhooksEnabled,omitempty"`

	// preferredNetworkInterface is the interface Plex uses, such as eth0. Empty
	// means any.
	// +optional
	PreferredNetworkInterface string `json:"preferredNetworkInterface,omitempty"`
}

// PlexConfigStatus defines the observed state of PlexConfig.
type PlexConfigStatus struct {
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	LastSyncTime       *metav1.Time       `json:"lastSyncTime,omitempty"`

	// managedResources records the resources this operator created, keyed by
	// resource type. Prune only removes entries listed here, so resources
	// created outside the operator are never deleted.
	// +optional
	ManagedResources map[string][]string `json:"managedResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=`.status.conditions[?(@.type=="Synced")].status`
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Last Sync",type="date",JSONPath=`.status.lastSyncTime`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`

// PlexConfig is the Schema for the plexconfigs API.
type PlexConfig struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec PlexConfigSpec `json:"spec"`

	// +optional
	Status PlexConfigStatus `json:"status,omitzero"`
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
func (c *PlexConfig) GetManagedResources() *map[string][]string           { return &c.Status.ManagedResources }
