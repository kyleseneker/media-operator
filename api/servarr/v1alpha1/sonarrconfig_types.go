package v1alpha1

import (
	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SonarrConfigSpec defines the desired state of SonarrConfig.
type SonarrConfigSpec struct {
	// connection defines how to connect to the Sonarr instance.
	// +required
	Connection commonv1alpha1.AppConnection `json:"connection"`

	// reconcile configures reconciliation behavior (e.g., sync interval).
	// +optional
	Reconcile *commonv1alpha1.ReconcileConfig `json:"reconcile,omitempty"`

	// mediaManagement configures media management settings.
	// +optional
	MediaManagement *SonarrMediaManagement `json:"mediaManagement,omitempty"`

	// naming configures episode and series naming formats.
	// +optional
	Naming *SonarrNaming `json:"naming,omitempty"`

	// indexerConfig configures global indexer settings.
	// +optional
	IndexerConfig *SonarrIndexerConfig `json:"indexerConfig,omitempty"`

	// downloadClientConfig configures global download client behavior.
	// +optional
	DownloadClientConfig *SonarrDownloadClientConfig `json:"downloadClientConfig,omitempty"`

	// ui configures UI display settings.
	// +optional
	UI *SonarrUI `json:"ui,omitempty"`

	// rootFolders defines media library root folder paths.
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

// SonarrMediaManagement configures how Sonarr handles media files.
type SonarrMediaManagement struct {
	// copyUsingHardlinks uses hardlinks instead of copies when importing.
	// +optional
	// +kubebuilder:default=true
	CopyUsingHardlinks *bool `json:"copyUsingHardlinks,omitempty"`

	// deleteEmptyFolders removes empty series folders after cleanup.
	// +optional
	// +kubebuilder:default=true
	DeleteEmptyFolders *bool `json:"deleteEmptyFolders,omitempty"`

	// importExtraFiles imports additional files (subtitles, etc.) alongside media.
	// +optional
	ImportExtraFiles *bool `json:"importExtraFiles,omitempty"`

	// extraFileExtensions is a comma-separated list of extra file extensions to import.
	// +optional
	ExtraFileExtensions string `json:"extraFileExtensions,omitempty"`

	// downloadPropersAndRepacks controls how proper/repack releases are handled.
	// +optional
	// +kubebuilder:validation:Enum=preferAndUpgrade;doNotPrefer;doNotUpgrade
	DownloadPropersAndRepacks string `json:"downloadPropersAndRepacks,omitempty"`

	// recycleBin is the path to the recycling bin folder. Empty disables.
	// +optional
	RecycleBin string `json:"recycleBin,omitempty"`

	// recycleBinCleanupDays is how many days files stay in the recycling bin.
	// +optional
	RecycleBinCleanupDays *int `json:"recycleBinCleanupDays,omitempty"`

	// minimumFreeSpaceWhenImporting is the minimum free space in MB required to import.
	// +optional
	MinimumFreeSpaceWhenImporting *int `json:"minimumFreeSpaceWhenImporting,omitempty"`

	// enableMediaInfo enables media info scanning.
	// +optional
	EnableMediaInfo *bool `json:"enableMediaInfo,omitempty"`

	// rescanAfterRefresh controls when to rescan series folders.
	// +optional
	// +kubebuilder:validation:Enum=always;afterManual;never
	RescanAfterRefresh string `json:"rescanAfterRefresh,omitempty"`

	// setPermissionsLinux enables setting file permissions on import.
	// +optional
	SetPermissionsLinux *bool `json:"setPermissionsLinux,omitempty"`

	// episodeTitleRequired controls when episode titles are required.
	// +optional
	// +kubebuilder:validation:Enum=always;bulkSeasonReleases;never
	EpisodeTitleRequired string `json:"episodeTitleRequired,omitempty"`

	// skipFreeSpaceCheckWhenImporting skips free space check during import.
	// +optional
	SkipFreeSpaceCheckWhenImporting *bool `json:"skipFreeSpaceCheckWhenImporting,omitempty"`

	// createEmptySeriesFolders creates folders for series with no files.
	// +optional
	CreateEmptySeriesFolders *bool `json:"createEmptySeriesFolders,omitempty"`
}

// SonarrNaming configures episode and folder naming conventions.
type SonarrNaming struct {
	// renameEpisodes enables automatic episode file renaming.
	// +optional
	RenameEpisodes *bool `json:"renameEpisodes,omitempty"`

	// replaceIllegalCharacters replaces illegal characters in filenames.
	// +optional
	ReplaceIllegalCharacters *bool `json:"replaceIllegalCharacters,omitempty"`

	// colonReplacementFormat defines how colons are replaced in filenames.
	// +optional
	ColonReplacementFormat *int `json:"colonReplacementFormat,omitempty"`

	// multiEpisodeStyle defines how multi-episode files are named.
	// +optional
	MultiEpisodeStyle *int `json:"multiEpisodeStyle,omitempty"`

	// standardEpisodeFormat is the naming format for standard episodes.
	// +optional
	StandardEpisodeFormat string `json:"standardEpisodeFormat,omitempty"`

	// dailyEpisodeFormat is the naming format for daily episodes.
	// +optional
	DailyEpisodeFormat string `json:"dailyEpisodeFormat,omitempty"`

	// animeEpisodeFormat is the naming format for anime episodes.
	// +optional
	AnimeEpisodeFormat string `json:"animeEpisodeFormat,omitempty"`

	// seriesFolderFormat is the naming format for series folders.
	// +optional
	SeriesFolderFormat string `json:"seriesFolderFormat,omitempty"`

	// seasonFolderFormat is the naming format for season folders.
	// +optional
	SeasonFolderFormat string `json:"seasonFolderFormat,omitempty"`

	// specialsFolderFormat is the naming format for specials folders.
	// +optional
	SpecialsFolderFormat string `json:"specialsFolderFormat,omitempty"`
}

// SonarrIndexerConfig configures global indexer settings.
type SonarrIndexerConfig struct {
	// minimumAge is the minimum age in minutes before downloading.
	// +optional
	MinimumAge *int `json:"minimumAge,omitempty"`

	// retention is the usenet retention in days. 0 means unlimited.
	// +optional
	Retention *int `json:"retention,omitempty"`

	// maximumSize is the maximum release size in MB. 0 means unlimited.
	// +optional
	MaximumSize *int `json:"maximumSize,omitempty"`

	// rssSyncInterval is the RSS sync interval in minutes. 0 disables RSS.
	// +optional
	RssSyncInterval *int `json:"rssSyncInterval,omitempty"`
}

// SonarrDownloadClientConfig configures global download client behavior.
type SonarrDownloadClientConfig struct {
	// enableCompletedDownloadHandling auto-imports completed downloads.
	// +optional
	// +kubebuilder:default=true
	EnableCompletedDownloadHandling *bool `json:"enableCompletedDownloadHandling,omitempty"`

	// autoRedownloadFailed automatically searches when a download fails.
	// +optional
	// +kubebuilder:default=true
	AutoRedownloadFailed *bool `json:"autoRedownloadFailed,omitempty"`

	// autoRedownloadFailedFromInteractiveSearch auto-searches failed interactive searches.
	// +optional
	AutoRedownloadFailedFromInteractiveSearch *bool `json:"autoRedownloadFailedFromInteractiveSearch,omitempty"`
}

// SonarrUI configures UI display settings.
type SonarrUI struct {
	// firstDayOfWeek is the first day of the week. 0=Sunday, 1=Monday.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=1
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

	// showRelativeDates shows relative dates (today, yesterday).
	// +optional
	ShowRelativeDates *bool `json:"showRelativeDates,omitempty"`

	// theme is the UI theme.
	// +optional
	// +kubebuilder:validation:Enum=auto;dark;light
	Theme string `json:"theme,omitempty"`

	// enableColorImpairedMode enables color-impaired accessibility mode.
	// +optional
	EnableColorImpairedMode *bool `json:"enableColorImpairedMode,omitempty"`

	// uiLanguage is the UI language ID. 1=English.
	// +optional
	UILanguage *int `json:"uiLanguage,omitempty"`
}

// SonarrConfigStatus defines the observed state of SonarrConfig.
type SonarrConfigStatus struct {
	// conditions represent the current state of the SonarrConfig resource.
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

// SonarrConfig is the Schema for the sonarrconfigs API.
type SonarrConfig struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec SonarrConfigSpec `json:"spec"`

	// +optional
	Status SonarrConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SonarrConfigList contains a list of SonarrConfig.
type SonarrConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SonarrConfig `json:"items"`
}

func (c *SonarrConfig) GetConditions() *[]metav1.Condition                  { return &c.Status.Conditions }
func (c *SonarrConfig) GetObservedGeneration() *int64                       { return &c.Status.ObservedGeneration }
func (c *SonarrConfig) GetLastSyncTime() **metav1.Time                      { return &c.Status.LastSyncTime }
func (c *SonarrConfig) GetReconcileConfig() *commonv1alpha1.ReconcileConfig { return c.Spec.Reconcile }

func init() {
	SchemeBuilder.Register(&SonarrConfig{}, &SonarrConfigList{})
}
