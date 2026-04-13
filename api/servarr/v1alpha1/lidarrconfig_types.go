package v1alpha1

import (
	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// LidarrConfigSpec defines the desired state of LidarrConfig.
type LidarrConfigSpec struct {
	// connection defines how to connect to the Lidarr instance.
	// +required
	Connection commonv1alpha1.AppConnection `json:"connection"`

	// reconcile configures reconciliation behavior (e.g., sync interval).
	// +optional
	Reconcile *commonv1alpha1.ReconcileConfig `json:"reconcile,omitempty"`

	// mediaManagement configures media management settings.
	// +optional
	MediaManagement *LidarrMediaManagement `json:"mediaManagement,omitempty"`

	// naming configures artist and track naming formats.
	// +optional
	Naming *LidarrNaming `json:"naming,omitempty"`

	// indexerConfig configures global indexer settings.
	// +optional
	IndexerConfig *LidarrIndexerConfig `json:"indexerConfig,omitempty"`

	// downloadClientConfig configures global download client behavior.
	// +optional
	DownloadClientConfig *LidarrDownloadClientConfig `json:"downloadClientConfig,omitempty"`

	// ui configures UI display settings.
	// +optional
	UI *LidarrUI `json:"ui,omitempty"`

	// rootFolders defines music library root folder paths.
	// +optional
	RootFolders []commonv1alpha1.RootFolder `json:"rootFolders,omitempty"`

	// downloadClients defines download client connections.
	// +optional
	DownloadClients []commonv1alpha1.DownloadClient `json:"downloadClients,omitempty"`

	// qualityProfiles defines quality profiles for controlling download quality preferences.
	// +optional
	QualityProfiles []commonv1alpha1.QualityProfile `json:"qualityProfiles,omitempty"`

	// customFormats defines custom format scoring rules for release selection.
	// +optional
	CustomFormats []commonv1alpha1.CustomFormat `json:"customFormats,omitempty"`

	// tags defines tags for linking resources together.
	// +optional
	Tags []commonv1alpha1.Tag `json:"tags,omitempty"`

	// indexers defines indexer connections.
	// +optional
	Indexers []commonv1alpha1.Indexer `json:"indexers,omitempty"`

	// notifications defines notification/connection configurations.
	// +optional
	Notifications []commonv1alpha1.Notification `json:"notifications,omitempty"`

	// importLists defines import list configurations for automatic content addition.
	// +optional
	ImportLists []commonv1alpha1.ImportList `json:"importLists,omitempty"`
}

// LidarrMediaManagement configures how Lidarr handles media files.
type LidarrMediaManagement struct {
	// copyUsingHardlinks uses hardlinks instead of copies when importing.
	// +optional
	CopyUsingHardlinks *bool `json:"copyUsingHardlinks,omitempty"`

	// deleteEmptyFolders removes empty artist folders after cleanup.
	// +optional
	DeleteEmptyFolders *bool `json:"deleteEmptyFolders,omitempty"`

	// importExtraFiles imports additional files alongside media.
	// +optional
	ImportExtraFiles *bool `json:"importExtraFiles,omitempty"`

	// extraFileExtensions is a comma-separated list of extra file extensions to import.
	// +optional
	ExtraFileExtensions string `json:"extraFileExtensions,omitempty"`

	// downloadPropersAndRepacks controls how proper/repack releases are handled.
	// +optional
	// +kubebuilder:validation:Enum=preferAndUpgrade;doNotPrefer;doNotUpgrade
	DownloadPropersAndRepacks string `json:"downloadPropersAndRepacks,omitempty"`

	// recycleBin is the path to the recycling bin folder.
	// +optional
	RecycleBin string `json:"recycleBin,omitempty"`

	// recycleBinCleanupDays is how many days files stay in the recycling bin.
	// +optional
	RecycleBinCleanupDays *int `json:"recycleBinCleanupDays,omitempty"`

	// minimumFreeSpaceWhenImporting is the minimum free space in MB required.
	// +optional
	MinimumFreeSpaceWhenImporting *int `json:"minimumFreeSpaceWhenImporting,omitempty"`

	// rescanAfterRefresh controls when to rescan artist folders.
	// +optional
	// +kubebuilder:validation:Enum=always;afterManual;never
	RescanAfterRefresh string `json:"rescanAfterRefresh,omitempty"`

	// setPermissionsLinux enables setting file permissions on import.
	// +optional
	SetPermissionsLinux *bool `json:"setPermissionsLinux,omitempty"`

	// createEmptyArtistFolders creates folders for artists with no files.
	// +optional
	CreateEmptyArtistFolders *bool `json:"createEmptyArtistFolders,omitempty"`
}

// LidarrNaming configures artist and track naming conventions.
type LidarrNaming struct {
	// renameTracks enables automatic track file renaming.
	// +optional
	RenameTracks *bool `json:"renameTracks,omitempty"`

	// replaceIllegalCharacters replaces illegal characters in filenames.
	// +optional
	ReplaceIllegalCharacters *bool `json:"replaceIllegalCharacters,omitempty"`

	// standardTrackFormat is the naming format for standard tracks.
	// +optional
	StandardTrackFormat string `json:"standardTrackFormat,omitempty"`

	// multiDiscTrackFormat is the naming format for multi-disc tracks.
	// +optional
	MultiDiscTrackFormat string `json:"multiDiscTrackFormat,omitempty"`

	// artistFolderFormat is the naming format for artist folders.
	// +optional
	ArtistFolderFormat string `json:"artistFolderFormat,omitempty"`
}

// LidarrIndexerConfig configures global indexer settings.
type LidarrIndexerConfig struct {
	// minimumAge is the minimum age in minutes before downloading.
	// +optional
	MinimumAge *int `json:"minimumAge,omitempty"`

	// retention is the usenet retention in days.
	// +optional
	Retention *int `json:"retention,omitempty"`

	// maximumSize is the maximum release size in MB.
	// +optional
	MaximumSize *int `json:"maximumSize,omitempty"`

	// rssSyncInterval is the RSS sync interval in minutes.
	// +optional
	RssSyncInterval *int `json:"rssSyncInterval,omitempty"`
}

// LidarrDownloadClientConfig configures global download client behavior.
type LidarrDownloadClientConfig struct {
	// enableCompletedDownloadHandling auto-imports completed downloads.
	// +optional
	EnableCompletedDownloadHandling *bool `json:"enableCompletedDownloadHandling,omitempty"`

	// autoRedownloadFailed automatically searches when a download fails.
	// +optional
	AutoRedownloadFailed *bool `json:"autoRedownloadFailed,omitempty"`
}

// LidarrUI configures UI display settings.
type LidarrUI struct {
	// firstDayOfWeek is the first day of the week. 0=Sunday, 1=Monday.
	// +optional
	FirstDayOfWeek *int `json:"firstDayOfWeek,omitempty"`

	// shortDateFormat is the short date display format.
	// +optional
	ShortDateFormat string `json:"shortDateFormat,omitempty"`

	// longDateFormat is the long date display format.
	// +optional
	LongDateFormat string `json:"longDateFormat,omitempty"`

	// timeFormat is the time display format.
	// +optional
	TimeFormat string `json:"timeFormat,omitempty"`

	// theme is the UI theme.
	// +optional
	// +kubebuilder:validation:Enum=auto;dark;light
	Theme string `json:"theme,omitempty"`

	// uiLanguage is the UI language ID.
	// +optional
	UILanguage *int `json:"uiLanguage,omitempty"`
}

// LidarrConfigStatus defines the observed state of LidarrConfig.
type LidarrConfigStatus struct {
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

// LidarrConfig is the Schema for the lidarrconfigs API.
type LidarrConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`
	Spec              LidarrConfigSpec   `json:"spec"`
	Status            LidarrConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// LidarrConfigList contains a list of LidarrConfig.
type LidarrConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []LidarrConfig `json:"items"`
}

func (c *LidarrConfig) GetConditions() *[]metav1.Condition                  { return &c.Status.Conditions }
func (c *LidarrConfig) GetObservedGeneration() *int64                       { return &c.Status.ObservedGeneration }
func (c *LidarrConfig) GetLastSyncTime() **metav1.Time                      { return &c.Status.LastSyncTime }
func (c *LidarrConfig) GetReconcileConfig() *commonv1alpha1.ReconcileConfig { return c.Spec.Reconcile }

func init() {
	SchemeBuilder.Register(&LidarrConfig{}, &LidarrConfigList{})
}
