package v1alpha1

import (
	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ReadarrConfigSpec defines the desired state of ReadarrConfig.
type ReadarrConfigSpec struct {
	// connection defines how to connect to the Readarr instance.
	// +required
	Connection commonv1alpha1.AppConnection `json:"connection"`

	// reconcile configures reconciliation behavior (e.g., sync interval).
	// +optional
	Reconcile *commonv1alpha1.ReconcileConfig `json:"reconcile,omitempty"`

	// mediaManagement configures media management settings.
	// +optional
	MediaManagement *ReadarrMediaManagement `json:"mediaManagement,omitempty"`

	// naming configures author and book naming formats.
	// +optional
	Naming *ReadarrNaming `json:"naming,omitempty"`

	// indexerConfig configures global indexer settings.
	// +optional
	IndexerConfig *ReadarrIndexerConfig `json:"indexerConfig,omitempty"`

	// downloadClientConfig configures global download client behavior.
	// +optional
	DownloadClientConfig *ReadarrDownloadClientConfig `json:"downloadClientConfig,omitempty"`

	// ui configures UI display settings.
	// +optional
	UI *ReadarrUI `json:"ui,omitempty"`

	// rootFolders defines book library root folder paths.
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

// ReadarrMediaManagement configures how Readarr handles media files.
type ReadarrMediaManagement struct {
	// copyUsingHardlinks uses hardlinks instead of copies when importing.
	// +optional
	CopyUsingHardlinks *bool `json:"copyUsingHardlinks,omitempty"`

	// deleteEmptyFolders removes empty author folders after cleanup.
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

	// rescanAfterRefresh controls when to rescan author folders.
	// +optional
	// +kubebuilder:validation:Enum=always;afterManual;never
	RescanAfterRefresh string `json:"rescanAfterRefresh,omitempty"`

	// setPermissionsLinux enables setting file permissions on import.
	// +optional
	SetPermissionsLinux *bool `json:"setPermissionsLinux,omitempty"`

	// createEmptyAuthorFolders creates folders for authors with no files.
	// +optional
	CreateEmptyAuthorFolders *bool `json:"createEmptyAuthorFolders,omitempty"`
}

// ReadarrNaming configures author and book naming conventions.
type ReadarrNaming struct {
	// renameBooks enables automatic book file renaming.
	// +optional
	RenameBooks *bool `json:"renameBooks,omitempty"`

	// replaceIllegalCharacters replaces illegal characters in filenames.
	// +optional
	ReplaceIllegalCharacters *bool `json:"replaceIllegalCharacters,omitempty"`

	// standardBookFormat is the naming format for book files.
	// +optional
	StandardBookFormat string `json:"standardBookFormat,omitempty"`

	// authorFolderFormat is the naming format for author folders.
	// +optional
	AuthorFolderFormat string `json:"authorFolderFormat,omitempty"`
}

// ReadarrIndexerConfig configures global indexer settings.
type ReadarrIndexerConfig struct {
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

// ReadarrDownloadClientConfig configures global download client behavior.
type ReadarrDownloadClientConfig struct {
	// enableCompletedDownloadHandling auto-imports completed downloads.
	// +optional
	EnableCompletedDownloadHandling *bool `json:"enableCompletedDownloadHandling,omitempty"`

	// autoRedownloadFailed automatically searches when a download fails.
	// +optional
	AutoRedownloadFailed *bool `json:"autoRedownloadFailed,omitempty"`
}

// ReadarrUI configures UI display settings.
type ReadarrUI struct {
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

// ReadarrConfigStatus defines the observed state of ReadarrConfig.
type ReadarrConfigStatus struct {
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

// ReadarrConfig is the Schema for the readarrconfigs API.
type ReadarrConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitzero"`
	Spec              ReadarrConfigSpec   `json:"spec"`
	Status            ReadarrConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ReadarrConfigList contains a list of ReadarrConfig.
type ReadarrConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []ReadarrConfig `json:"items"`
}

func (c *ReadarrConfig) GetConditions() *[]metav1.Condition                  { return &c.Status.Conditions }
func (c *ReadarrConfig) GetObservedGeneration() *int64                       { return &c.Status.ObservedGeneration }
func (c *ReadarrConfig) GetLastSyncTime() **metav1.Time                      { return &c.Status.LastSyncTime }
func (c *ReadarrConfig) GetReconcileConfig() *commonv1alpha1.ReconcileConfig { return c.Spec.Reconcile }

func init() {
	SchemeBuilder.Register(&ReadarrConfig{}, &ReadarrConfigList{})
}
