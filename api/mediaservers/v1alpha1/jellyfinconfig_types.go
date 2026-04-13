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

// JellyfinConfigSpec defines the desired state of JellyfinConfig.
type JellyfinConfigSpec struct {
	// connection defines how to connect to the Jellyfin instance.
	// +required
	Connection JellyfinConnection `json:"connection"`

	// reconcile configures reconciliation behavior (e.g., sync interval).
	// +optional
	Reconcile *commonv1alpha1.ReconcileConfig `json:"reconcile,omitempty"`

	// adminUser defines the admin credentials used for initial setup and API access.
	// +required
	AdminUser JellyfinAdminUser `json:"adminUser"`

	// server configures Jellyfin server settings.
	// +optional
	Server *JellyfinServer `json:"server,omitempty"`

	// encoding configures hardware-accelerated transcoding settings.
	// +optional
	Encoding *JellyfinEncoding `json:"encoding,omitempty"`

	// libraries defines media libraries to create and configure.
	// +optional
	Libraries []JellyfinLibrary `json:"libraries,omitempty"`
}

// JellyfinConnection defines how the operator connects to Jellyfin.
// Jellyfin uses admin credentials rather than an API key.
type JellyfinConnection struct {
	// url is the base URL of the Jellyfin instance (e.g., http://jellyfin.media.svc.cluster.local:8096).
	// +required
	URL string `json:"url"`

	// tls configures TLS settings for the connection.
	// Only needed when Jellyfin uses HTTPS with self-signed or private CA certificates.
	// +optional
	TLS *commonv1alpha1.TLSConfig `json:"tls,omitempty"`
}

// JellyfinAdminUser defines the admin user credentials for Jellyfin.
type JellyfinAdminUser struct {
	// usernameSecretRef references a Secret containing the admin username.
	// +required
	UsernameSecretRef commonv1alpha1.SecretKeyRef `json:"usernameSecretRef"`

	// passwordSecretRef references a Secret containing the admin password.
	// +required
	PasswordSecretRef commonv1alpha1.SecretKeyRef `json:"passwordSecretRef"`
}

// JellyfinServer configures Jellyfin server settings.
type JellyfinServer struct {
	// serverName is the friendly name of the server.
	// +optional
	ServerName string `json:"serverName,omitempty"`

	// preferredMetadataLanguage is the preferred language for metadata (e.g., "en").
	// +optional
	PreferredMetadataLanguage string `json:"preferredMetadataLanguage,omitempty"`

	// metadataCountryCode is the country code for metadata lookups (e.g., "US").
	// +optional
	MetadataCountryCode string `json:"metadataCountryCode,omitempty"`

	// logFileRetentionDays is the number of days to retain log files.
	// +optional
	LogFileRetentionDays *int `json:"logFileRetentionDays,omitempty"`

	// enableGroupingMoviesIntoCollections enables automatic grouping of movies into collections.
	// +optional
	EnableGroupingMoviesIntoCollections *bool `json:"enableGroupingMoviesIntoCollections,omitempty"`

	// displaySpecialsWithinSeasons displays special episodes within their respective seasons.
	// +optional
	DisplaySpecialsWithinSeasons *bool `json:"displaySpecialsWithinSeasons,omitempty"`

	// libraryMonitorDelay is the delay in seconds before processing library changes.
	// +optional
	LibraryMonitorDelay *int `json:"libraryMonitorDelay,omitempty"`
}

// JellyfinEncoding configures hardware-accelerated transcoding settings.
type JellyfinEncoding struct {
	// hardwareAccelerationType is the hardware acceleration method to use.
	// +optional
	// +kubebuilder:validation:Enum=none;vaapi;qsv;nvenc
	HardwareAccelerationType string `json:"hardwareAccelerationType,omitempty"`

	// vaapiDevice is the VAAPI render device path (e.g., "/dev/dri/renderD128").
	// +optional
	VaapiDevice string `json:"vaapiDevice,omitempty"`

	// enableHardwareEncoding enables hardware-accelerated encoding.
	// +optional
	EnableHardwareEncoding *bool `json:"enableHardwareEncoding,omitempty"`

	// allowHevcEncoding allows HEVC/H.265 hardware encoding.
	// +optional
	AllowHevcEncoding *bool `json:"allowHevcEncoding,omitempty"`

	// enableTonemapping enables HDR to SDR tone mapping.
	// +optional
	EnableTonemapping *bool `json:"enableTonemapping,omitempty"`

	// enableVppTonemapping enables VPP-based tone mapping (Intel QSV).
	// +optional
	EnableVppTonemapping *bool `json:"enableVppTonemapping,omitempty"`

	// h264Crf is the CRF value for H.264 encoding.
	// +optional
	H264Crf *int `json:"h264Crf,omitempty"`

	// h265Crf is the CRF value for H.265/HEVC encoding.
	// +optional
	H265Crf *int `json:"h265Crf,omitempty"`

	// encoderPreset is the encoder speed preset (e.g., "auto", "fast", "medium", "slow").
	// +optional
	EncoderPreset string `json:"encoderPreset,omitempty"`

	// hardwareDecodingCodecs is the list of codecs to decode in hardware.
	// +optional
	HardwareDecodingCodecs []string `json:"hardwareDecodingCodecs,omitempty"`

	// enableIntelLowPowerH264HwEncoder enables the Intel low-power H.264 hardware encoder.
	// +optional
	EnableIntelLowPowerH264HwEncoder *bool `json:"enableIntelLowPowerH264HwEncoder,omitempty"`

	// enableIntelLowPowerHevcHwEncoder enables the Intel low-power HEVC hardware encoder.
	// +optional
	EnableIntelLowPowerHevcHwEncoder *bool `json:"enableIntelLowPowerHevcHwEncoder,omitempty"`

	// enableSubtitleExtraction enables extracting subtitles from media files.
	// +optional
	EnableSubtitleExtraction *bool `json:"enableSubtitleExtraction,omitempty"`

	// enableThrottling enables transcoding throttling.
	// +optional
	EnableThrottling *bool `json:"enableThrottling,omitempty"`

	// throttleDelaySeconds is the delay in seconds before throttling begins.
	// +optional
	ThrottleDelaySeconds *int `json:"throttleDelaySeconds,omitempty"`
}

// JellyfinLibrary defines a media library to create in Jellyfin.
type JellyfinLibrary struct {
	// name is the display name of the library.
	// +required
	Name string `json:"name"`

	// collectionType is the type of media in this library.
	// +required
	// +kubebuilder:validation:Enum=movies;tvshows;music;musicvideos;homevideos;books;mixed
	CollectionType string `json:"collectionType"`

	// paths is the list of filesystem paths containing media for this library.
	// +required
	Paths []string `json:"paths"`

	// enableRealtimeMonitor enables real-time monitoring of library paths for changes.
	// +optional
	EnableRealtimeMonitor *bool `json:"enableRealtimeMonitor,omitempty"`

	// enableTrickplayImageExtraction enables generation of trickplay (scrubbing preview) images.
	// +optional
	EnableTrickplayImageExtraction *bool `json:"enableTrickplayImageExtraction,omitempty"`

	// automaticRefreshIntervalDays is the number of days between automatic library scans.
	// +optional
	AutomaticRefreshIntervalDays *int `json:"automaticRefreshIntervalDays,omitempty"`

	// preferredMetadataLanguage overrides the server-level preferred metadata language for this library.
	// +optional
	PreferredMetadataLanguage string `json:"preferredMetadataLanguage,omitempty"`

	// metadataCountryCode overrides the server-level metadata country code for this library.
	// +optional
	MetadataCountryCode string `json:"metadataCountryCode,omitempty"`
}

// JellyfinConfigStatus defines the observed state of JellyfinConfig.
type JellyfinConfigStatus struct {
	// conditions represent the current state of the JellyfinConfig resource.
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

	// initialized indicates whether the Jellyfin setup wizard has been completed.
	// +optional
	Initialized *bool `json:"initialized,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Synced",type="string",JSONPath=`.status.conditions[?(@.type=="Synced")].status`
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=`.status.conditions[?(@.type=="Ready")].status`
// +kubebuilder:printcolumn:name="Last Sync",type="date",JSONPath=`.status.lastSyncTime`
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=`.metadata.creationTimestamp`

// JellyfinConfig is the Schema for the jellyfinconfigs API.
type JellyfinConfig struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec JellyfinConfigSpec `json:"spec"`

	// +optional
	Status JellyfinConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// JellyfinConfigList contains a list of JellyfinConfig.
type JellyfinConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []JellyfinConfig `json:"items"`
}

func (c *JellyfinConfig) GetConditions() *[]metav1.Condition { return &c.Status.Conditions }
func (c *JellyfinConfig) GetObservedGeneration() *int64      { return &c.Status.ObservedGeneration }
func (c *JellyfinConfig) GetLastSyncTime() **metav1.Time     { return &c.Status.LastSyncTime }
func (c *JellyfinConfig) GetReconcileConfig() *commonv1alpha1.ReconcileConfig {
	return c.Spec.Reconcile
}

func init() {
	SchemeBuilder.Register(&JellyfinConfig{}, &JellyfinConfigList{})
}
