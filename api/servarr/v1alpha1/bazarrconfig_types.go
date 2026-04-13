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

// BazarrConfigSpec defines the desired state of BazarrConfig.
type BazarrConfigSpec struct {
	// connection defines how to connect to the Bazarr instance.
	// +required
	Connection commonv1alpha1.AppConnection `json:"connection"`

	// reconcile configures reconciliation behavior (e.g., sync interval).
	// +optional
	Reconcile *commonv1alpha1.ReconcileConfig `json:"reconcile,omitempty"`

	// general configures general Bazarr settings.
	// +optional
	General *BazarrGeneral `json:"general,omitempty"`

	// sonarrConnection configures the connection to a Sonarr instance.
	// +optional
	SonarrConnection *BazarrAppConnection `json:"sonarrConnection,omitempty"`

	// radarrConnection configures the connection to a Radarr instance.
	// +optional
	RadarrConnection *BazarrAppConnection `json:"radarrConnection,omitempty"`

	// subSync configures subtitle synchronization settings.
	// +optional
	SubSync *BazarrSubSync `json:"subSync,omitempty"`

	// providers defines subtitle provider configurations.
	// +optional
	Providers []BazarrProvider `json:"providers,omitempty"`

	// languages configures language settings and profiles.
	// +optional
	Languages *BazarrLanguages `json:"languages,omitempty"`
}

// BazarrGeneral configures general Bazarr settings.
type BazarrGeneral struct {
	// useSonarr enables the Sonarr integration.
	// +optional
	UseSonarr *bool `json:"useSonarr,omitempty"`

	// useRadarr enables the Radarr integration.
	// +optional
	UseRadarr *bool `json:"useRadarr,omitempty"`

	// minimumScore is the minimum subtitle match score for series.
	// +optional
	MinimumScore *int `json:"minimumScore,omitempty"`

	// minimumScoreMovie is the minimum subtitle match score for movies.
	// +optional
	MinimumScoreMovie *int `json:"minimumScoreMovie,omitempty"`

	// useScenename uses scene names for subtitle searching.
	// +optional
	UseScenename *bool `json:"useScenename,omitempty"`

	// usePostprocessing enables post-processing of downloaded subtitles.
	// +optional
	UsePostprocessing *bool `json:"usePostprocessing,omitempty"`

	// adaptiveSearching enables adaptive searching for subtitles.
	// +optional
	AdaptiveSearching *bool `json:"adaptiveSearching,omitempty"`

	// multithreading enables multithreaded subtitle downloading.
	// +optional
	Multithreading *bool `json:"multithreading,omitempty"`

	// chmodEnabled enables setting file permissions on subtitle files.
	// +optional
	ChmodEnabled *bool `json:"chmodEnabled,omitempty"`

	// upgradeSubs enables automatic subtitle upgrades when better ones are found.
	// +optional
	UpgradeSubs *bool `json:"upgradeSubs,omitempty"`

	// upgradeFrequency is the frequency in hours to check for subtitle upgrades.
	// +optional
	UpgradeFrequency *int `json:"upgradeFrequency,omitempty"`

	// wantedSearchFrequency is the frequency in hours to search for wanted subtitles.
	// +optional
	WantedSearchFrequency *int `json:"wantedSearchFrequency,omitempty"`

	// utf8Encode encodes subtitles to UTF-8.
	// +optional
	Utf8Encode *bool `json:"utf8Encode,omitempty"`

	// ignorePgsSubs ignores PGS (image-based) subtitles.
	// +optional
	IgnorePgsSubs *bool `json:"ignorePgsSubs,omitempty"`

	// ignoreVobsubSubs ignores VobSub (image-based) subtitles.
	// +optional
	IgnoreVobsubSubs *bool `json:"ignoreVobsubSubs,omitempty"`

	// ignoreAssSubs ignores ASS/SSA subtitles.
	// +optional
	IgnoreAssSubs *bool `json:"ignoreAssSubs,omitempty"`

	// embeddedSubtitlesParser is the parser used for embedded subtitles.
	// +optional
	EmbeddedSubtitlesParser string `json:"embeddedSubtitlesParser,omitempty"`

	// hiExtension is the file extension used for hearing-impaired subtitle files.
	// +optional
	HiExtension string `json:"hiExtension,omitempty"`
}

// BazarrAppConnection defines a connection to a Sonarr or Radarr instance from Bazarr.
type BazarrAppConnection struct {
	// host is the hostname or IP of the application.
	// +required
	Host string `json:"host"`

	// port is the port of the application.
	// +required
	Port *int `json:"port,omitempty"`

	// basePath is the URL base path of the application.
	// +optional
	BasePath string `json:"basePath,omitempty"`

	// ssl enables HTTPS connections to the application.
	// +optional
	SSL *bool `json:"ssl,omitempty"`

	// apiKeySecretRef references a Secret containing the application's API key.
	// +required
	APIKeySecretRef commonv1alpha1.SecretKeyRef `json:"apiKeySecretRef"`

	// httpTimeout is the HTTP request timeout in seconds.
	// +optional
	HttpTimeout *int `json:"httpTimeout,omitempty"`

	// fullUpdate controls how often a full library sync is performed.
	// +optional
	// +kubebuilder:validation:Enum=Daily;Weekly;Manually
	FullUpdate string `json:"fullUpdate,omitempty"`

	// onlyMonitored limits subtitle searches to monitored items only.
	// +optional
	OnlyMonitored *bool `json:"onlyMonitored,omitempty"`

	// syncInterval is the sync interval in hours.
	// +optional
	SyncInterval *int `json:"syncInterval,omitempty"`
}

// BazarrSubSync configures subtitle synchronization settings.
type BazarrSubSync struct {
	// useSubsync enables automatic subtitle synchronization.
	// +optional
	UseSubsync *bool `json:"useSubsync,omitempty"`

	// useSubsyncThreshold enables a score threshold for series subtitle sync.
	// +optional
	UseSubsyncThreshold *bool `json:"useSubsyncThreshold,omitempty"`

	// subsyncThreshold is the minimum score threshold for series subtitle sync.
	// +optional
	SubsyncThreshold *int `json:"subsyncThreshold,omitempty"`

	// useSubsyncMovieThreshold enables a score threshold for movie subtitle sync.
	// +optional
	UseSubsyncMovieThreshold *bool `json:"useSubsyncMovieThreshold,omitempty"`

	// subsyncMovieThreshold is the minimum score threshold for movie subtitle sync.
	// +optional
	SubsyncMovieThreshold *int `json:"subsyncMovieThreshold,omitempty"`
}

// BazarrProvider defines a subtitle provider configuration.
type BazarrProvider struct {
	// name is the provider key (e.g., opensubtitlescom, addic7ed).
	// +required
	Name string `json:"name"`

	// enabled controls whether this provider is active.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// settings is a map of provider-specific settings. Values starting with "ENV:" are substituted from secrets.
	// +optional
	Settings map[string]string `json:"settings,omitempty"`
}

// BazarrLanguages configures language settings and profiles.
type BazarrLanguages struct {
	// enabled is the list of enabled subtitle languages as ISO 639-1 codes.
	// +optional
	Enabled []string `json:"enabled,omitempty"`

	// profiles defines language profiles for subtitle selection.
	// +optional
	Profiles []BazarrLanguageProfile `json:"profiles,omitempty"`
}

// BazarrLanguageProfile defines a language profile for subtitle selection.
type BazarrLanguageProfile struct {
	// profileId is the unique identifier for this profile.
	// +required
	ProfileId *int `json:"profileId,omitempty"`

	// name is the display name of the language profile.
	// +required
	Name string `json:"name"`

	// items defines the language items in this profile.
	// +optional
	Items []BazarrLanguageItem `json:"items,omitempty"`
}

// BazarrLanguageItem defines a language entry within a language profile.
type BazarrLanguageItem struct {
	// language is the ISO 639-1 language code.
	// +required
	Language string `json:"language"`

	// hi indicates whether hearing-impaired subtitles are preferred.
	// +optional
	HI *bool `json:"hi,omitempty"`

	// forced indicates whether forced subtitles are preferred.
	// +optional
	Forced *bool `json:"forced,omitempty"`

	// audioExclude excludes this language if audio already matches.
	// +optional
	AudioExclude *bool `json:"audioExclude,omitempty"`
}

// BazarrConfigStatus defines the observed state of BazarrConfig.
type BazarrConfigStatus struct {
	// conditions represent the current state of the BazarrConfig resource.
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

// BazarrConfig is the Schema for the bazarrconfigs API.
type BazarrConfig struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec BazarrConfigSpec `json:"spec"`

	// +optional
	Status BazarrConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// BazarrConfigList contains a list of BazarrConfig.
type BazarrConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []BazarrConfig `json:"items"`
}

func (c *BazarrConfig) GetConditions() *[]metav1.Condition                  { return &c.Status.Conditions }
func (c *BazarrConfig) GetObservedGeneration() *int64                       { return &c.Status.ObservedGeneration }
func (c *BazarrConfig) GetLastSyncTime() **metav1.Time                      { return &c.Status.LastSyncTime }
func (c *BazarrConfig) GetReconcileConfig() *commonv1alpha1.ReconcileConfig { return c.Spec.Reconcile }

func init() {
	SchemeBuilder.Register(&BazarrConfig{}, &BazarrConfigList{})
}
