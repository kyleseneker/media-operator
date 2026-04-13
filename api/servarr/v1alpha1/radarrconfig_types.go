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

// RadarrConfigSpec defines the desired state of RadarrConfig.
type RadarrConfigSpec struct {
	// connection defines how to connect to the Radarr instance.
	// +required
	Connection commonv1alpha1.AppConnection `json:"connection"`

	// reconcile configures reconciliation behavior (e.g., sync interval).
	// +optional
	Reconcile *commonv1alpha1.ReconcileConfig `json:"reconcile,omitempty"`

	// mediaManagement configures media management settings.
	// +optional
	MediaManagement *RadarrMediaManagement `json:"mediaManagement,omitempty"`

	// naming configures movie and folder naming formats.
	// +optional
	Naming *RadarrNaming `json:"naming,omitempty"`

	// indexerConfig configures global indexer settings.
	// +optional
	IndexerConfig *RadarrIndexerConfig `json:"indexerConfig,omitempty"`

	// downloadClientConfig configures global download client behavior.
	// +optional
	DownloadClientConfig *RadarrDownloadClientConfig `json:"downloadClientConfig,omitempty"`

	// ui configures UI display settings.
	// +optional
	UI *RadarrUI `json:"ui,omitempty"`

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

// RadarrMediaManagement configures how Radarr handles media files.
type RadarrMediaManagement struct {
	// autoUnmonitorPreviouslyDownloadedMovies unmonitors movies that have been downloaded.
	// +optional
	AutoUnmonitorPreviouslyDownloadedMovies *bool `json:"autoUnmonitorPreviouslyDownloadedMovies,omitempty"`

	// createEmptyMovieFolders creates folders for movies with no files.
	// +optional
	CreateEmptyMovieFolders *bool `json:"createEmptyMovieFolders,omitempty"`

	// autoRenameFolders automatically renames movie folders when the movie title changes.
	// +optional
	AutoRenameFolders *bool `json:"autoRenameFolders,omitempty"`

	// pathsDefaultStatic prevents Radarr from updating paths when the root folder changes.
	// +optional
	PathsDefaultStatic *bool `json:"pathsDefaultStatic,omitempty"`

	// copyUsingHardlinks uses hardlinks instead of copies when importing.
	// +optional
	// +kubebuilder:default=true
	CopyUsingHardlinks *bool `json:"copyUsingHardlinks,omitempty"`

	// deleteEmptyFolders removes empty movie folders after cleanup.
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

	// rescanAfterRefresh controls when to rescan movie folders.
	// +optional
	// +kubebuilder:validation:Enum=always;afterManual;never
	RescanAfterRefresh string `json:"rescanAfterRefresh,omitempty"`

	// setPermissionsLinux enables setting file permissions on import.
	// +optional
	SetPermissionsLinux *bool `json:"setPermissionsLinux,omitempty"`

	// skipFreeSpaceCheckWhenImporting skips free space check during import.
	// +optional
	SkipFreeSpaceCheckWhenImporting *bool `json:"skipFreeSpaceCheckWhenImporting,omitempty"`
}

// RadarrNaming configures movie and folder naming conventions.
type RadarrNaming struct {
	// renameMovies enables automatic movie file renaming.
	// +optional
	RenameMovies *bool `json:"renameMovies,omitempty"`

	// replaceIllegalCharacters replaces illegal characters in filenames.
	// +optional
	ReplaceIllegalCharacters *bool `json:"replaceIllegalCharacters,omitempty"`

	// colonReplacementFormat defines how colons are replaced in filenames.
	// +optional
	// +kubebuilder:validation:Enum=delete;dash;spaceDash;spaceDashSpace;smart
	ColonReplacementFormat string `json:"colonReplacementFormat,omitempty"`

	// standardMovieFormat is the naming format for movies.
	// +optional
	StandardMovieFormat string `json:"standardMovieFormat,omitempty"`

	// movieFolderFormat is the naming format for movie folders.
	// +optional
	MovieFolderFormat string `json:"movieFolderFormat,omitempty"`
}

// RadarrIndexerConfig configures global indexer settings.
type RadarrIndexerConfig struct {
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

	// preferIndexerFlags prefers releases with indexer flags.
	// +optional
	PreferIndexerFlags *bool `json:"preferIndexerFlags,omitempty"`

	// availabilityDelay is the number of days to wait after a movie is available before searching.
	// +optional
	AvailabilityDelay *int `json:"availabilityDelay,omitempty"`

	// allowHardcodedSubs allows importing releases with hardcoded subtitles.
	// +optional
	AllowHardcodedSubs *bool `json:"allowHardcodedSubs,omitempty"`

	// whitelistedHardcodedSubs is a comma-separated list of subtitle languages to allow when hardcoded.
	// +optional
	WhitelistedHardcodedSubs string `json:"whitelistedHardcodedSubs,omitempty"`
}

// RadarrDownloadClientConfig configures global download client behavior.
type RadarrDownloadClientConfig struct {
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

	// checkForFinishedDownloadInterval is the interval in minutes to check for finished downloads.
	// +optional
	CheckForFinishedDownloadInterval *int `json:"checkForFinishedDownloadInterval,omitempty"`
}

// RadarrUI configures UI display settings.
type RadarrUI struct {
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

	// movieRuntimeFormat controls how movie runtimes are displayed.
	// +optional
	// +kubebuilder:validation:Enum=hoursMinutes;minutes
	MovieRuntimeFormat string `json:"movieRuntimeFormat,omitempty"`

	// movieInfoLanguage is the language ID for movie information. -1=Original.
	// +optional
	MovieInfoLanguage *int `json:"movieInfoLanguage,omitempty"`
}

// RadarrConfigStatus defines the observed state of RadarrConfig.
type RadarrConfigStatus struct {
	// conditions represent the current state of the RadarrConfig resource.
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

// RadarrConfig is the Schema for the radarrconfigs API.
type RadarrConfig struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec RadarrConfigSpec `json:"spec"`

	// +optional
	Status RadarrConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// RadarrConfigList contains a list of RadarrConfig.
type RadarrConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []RadarrConfig `json:"items"`
}

func (c *RadarrConfig) GetConditions() *[]metav1.Condition                  { return &c.Status.Conditions }
func (c *RadarrConfig) GetObservedGeneration() *int64                       { return &c.Status.ObservedGeneration }
func (c *RadarrConfig) GetLastSyncTime() **metav1.Time                      { return &c.Status.LastSyncTime }
func (c *RadarrConfig) GetReconcileConfig() *commonv1alpha1.ReconcileConfig { return c.Spec.Reconcile }

func init() {
	SchemeBuilder.Register(&RadarrConfig{}, &RadarrConfigList{})
}
