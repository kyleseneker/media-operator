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

	// network configures how Jellyfin tells local clients from remote ones and
	// which proxies it trusts.
	// +optional
	Network *JellyfinNetwork `json:"network,omitempty"`

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

	// enableMetrics serves Prometheus metrics at /metrics.
	// +optional
	EnableMetrics *bool `json:"enableMetrics,omitempty"`

	// quickConnectAvailable lets users sign in on a new device with a code.
	// +optional
	QuickConnectAvailable *bool `json:"quickConnectAvailable,omitempty"`

	// remoteClientBitrateLimit caps the bitrate of each remote stream, in bits
	// per second. 0 means no limit.
	// +optional
	// +kubebuilder:validation:Minimum=0
	RemoteClientBitrateLimit *int `json:"remoteClientBitrateLimit,omitempty"`

	// enableGroupingShowsIntoCollections shows series in collections instead of
	// on their own.
	// +optional
	EnableGroupingShowsIntoCollections *bool `json:"enableGroupingShowsIntoCollections,omitempty"`

	// enableFolderView shows a Folders view of the media folders.
	// +optional
	EnableFolderView *bool `json:"enableFolderView,omitempty"`

	// enableExternalContentInSuggestions includes internet trailers and live TV
	// in suggestions.
	// +optional
	EnableExternalContentInSuggestions *bool `json:"enableExternalContentInSuggestions,omitempty"`

	// minResumePct is how far, in percent, playback must get before it can be resumed.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	MinResumePct *int `json:"minResumePct,omitempty"`

	// maxResumePct is how far, in percent, playback must get to count as played.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	MaxResumePct *int `json:"maxResumePct,omitempty"`

	// minResumeDurationSeconds is the shortest item, in seconds, that can be resumed.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MinResumeDurationSeconds *int `json:"minResumeDurationSeconds,omitempty"`

	// minAudiobookResume is how many minutes an audiobook must play before it
	// can be resumed.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MinAudiobookResume *int `json:"minAudiobookResume,omitempty"`

	// maxAudiobookResume is how many minutes from the end an audiobook counts as played.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxAudiobookResume *int `json:"maxAudiobookResume,omitempty"`

	// activityLogRetentionDays is how many days the activity log keeps entries.
	// +optional
	// +kubebuilder:validation:Minimum=0
	ActivityLogRetentionDays *int `json:"activityLogRetentionDays,omitempty"`

	// libraryScanFanoutConcurrency is how many library scan tasks run at once.
	// 0 picks one from the CPU count.
	// +optional
	// +kubebuilder:validation:Minimum=0
	LibraryScanFanoutConcurrency *int `json:"libraryScanFanoutConcurrency,omitempty"`

	// libraryMetadataRefreshConcurrency is how many metadata refreshes run at
	// once. 0 picks one from the CPU count.
	// +optional
	// +kubebuilder:validation:Minimum=0
	LibraryMetadataRefreshConcurrency *int `json:"libraryMetadataRefreshConcurrency,omitempty"`

	// parallelImageEncodingLimit is how many images are encoded at once. 0
	// picks one from the CPU count.
	// +optional
	// +kubebuilder:validation:Minimum=0
	ParallelImageEncodingLimit *int `json:"parallelImageEncodingLimit,omitempty"`

	// enableSlowResponseWarning logs requests slower than slowResponseThresholdMs.
	// +optional
	EnableSlowResponseWarning *bool `json:"enableSlowResponseWarning,omitempty"`

	// slowResponseThresholdMs is how many milliseconds a request takes before
	// it's logged as slow.
	// +optional
	// +kubebuilder:validation:Minimum=0
	SlowResponseThresholdMs *int `json:"slowResponseThresholdMs,omitempty"`

	// imageSavingConvention is how Jellyfin names the artwork it saves with media.
	// +optional
	// +kubebuilder:validation:Enum=Legacy;Compatible
	ImageSavingConvention string `json:"imageSavingConvention,omitempty"`

	// trickplayOptions configures the preview thumbnails shown while seeking.
	// +optional
	TrickplayOptions *JellyfinTrickplay `json:"trickplayOptions,omitempty"`
}

// JellyfinTrickplay configures how Jellyfin makes trickplay thumbnails.
type JellyfinTrickplay struct {
	// enableHwAcceleration decodes video in hardware while making thumbnails.
	// +optional
	EnableHwAcceleration *bool `json:"enableHwAcceleration,omitempty"`

	// enableHwEncoding encodes thumbnails in hardware.
	// +optional
	EnableHwEncoding *bool `json:"enableHwEncoding,omitempty"`

	// enableKeyFrameOnlyExtraction takes thumbnails only at key frames, which is
	// faster and less precise.
	// +optional
	EnableKeyFrameOnlyExtraction *bool `json:"enableKeyFrameOnlyExtraction,omitempty"`

	// scanBehavior decides whether a library scan waits for thumbnails.
	// +optional
	// +kubebuilder:validation:Enum=Blocking;NonBlocking
	ScanBehavior string `json:"scanBehavior,omitempty"`

	// processPriority is the priority of the process making thumbnails.
	// +optional
	// +kubebuilder:validation:Enum=Idle;BelowNormal;Normal;AboveNormal;High
	ProcessPriority string `json:"processPriority,omitempty"`

	// interval is the time between thumbnails, in milliseconds.
	// +optional
	// +kubebuilder:validation:Minimum=1
	Interval *int `json:"interval,omitempty"`

	// widthResolutions are the thumbnail widths to make, in pixels.
	// +optional
	WidthResolutions []int `json:"widthResolutions,omitempty"`

	// tileWidth is how many thumbnails go across each tile image.
	// +optional
	// +kubebuilder:validation:Minimum=1
	TileWidth *int `json:"tileWidth,omitempty"`

	// tileHeight is how many thumbnails go down each tile image.
	// +optional
	// +kubebuilder:validation:Minimum=1
	TileHeight *int `json:"tileHeight,omitempty"`

	// qscale is the thumbnails' encoding quality, from 2 (best) to 31.
	// +optional
	// +kubebuilder:validation:Minimum=2
	// +kubebuilder:validation:Maximum=31
	Qscale *int `json:"qscale,omitempty"`

	// jpegQuality is the JPEG quality of the tile images.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=100
	JpegQuality *int `json:"jpegQuality,omitempty"`

	// processThreads is how many threads making thumbnails uses.
	// +optional
	// +kubebuilder:validation:Minimum=0
	ProcessThreads *int `json:"processThreads,omitempty"`
}

// JellyfinNetwork configures Jellyfin's network settings. Ports, the base URL,
// HTTPS and bind addresses aren't here: changing them needs a restart and has
// to match the Service and connection.url.
type JellyfinNetwork struct {
	// enableRemoteAccess allows connections from outside localNetworkSubnets.
	// With it off, localNetworkSubnets must include the operator's pod network.
	// +optional
	EnableRemoteAccess *bool `json:"enableRemoteAccess,omitempty"`

	// localNetworkSubnets are the networks, in CIDR form, whose clients count as
	// local. Empty means the private address ranges.
	// +optional
	LocalNetworkSubnets []string `json:"localNetworkSubnets,omitempty"`

	// knownProxies are the reverse proxies, by address or host name, whose
	// forwarded headers Jellyfin trusts for the client's address.
	// +optional
	KnownProxies []string `json:"knownProxies,omitempty"`

	// autoDiscovery answers clients looking for servers on the local network.
	// +optional
	AutoDiscovery *bool `json:"autoDiscovery,omitempty"`

	// enableUPnP forwards Jellyfin's public ports on the router.
	// +optional
	EnableUPnP *bool `json:"enableUPnP,omitempty"`

	// ignoreVirtualInterfaces leaves the interfaces named in
	// virtualInterfaceNames out of auto-detection.
	// +optional
	IgnoreVirtualInterfaces *bool `json:"ignoreVirtualInterfaces,omitempty"`

	// virtualInterfaceNames are interface name prefixes, such as veth, that
	// ignoreVirtualInterfaces skips.
	// +optional
	VirtualInterfaceNames []string `json:"virtualInterfaceNames,omitempty"`

	// enablePublishedServerUriByRequest tells clients the address they reached
	// Jellyfin on, instead of one Jellyfin works out.
	// +optional
	EnablePublishedServerUriByRequest *bool `json:"enablePublishedServerUriByRequest,omitempty"`

	// publishedServerUriBySubnet overrides the address Jellyfin gives clients,
	// as subnet=URL entries, such as all=https://jellyfin.example.com.
	// +optional
	PublishedServerUriBySubnet []string `json:"publishedServerUriBySubnet,omitempty"`

	// remoteIPFilter lists remote addresses or subnets to allow, or to block when
	// isRemoteIPFilterBlacklist is on.
	// +optional
	RemoteIPFilter []string `json:"remoteIPFilter,omitempty"`

	// isRemoteIPFilterBlacklist blocks the addresses in remoteIPFilter instead
	// of allowing only them.
	// +optional
	IsRemoteIPFilterBlacklist *bool `json:"isRemoteIPFilterBlacklist,omitempty"`
}

// JellyfinEncoding configures hardware-accelerated transcoding settings.
type JellyfinEncoding struct {
	// hardwareAccelerationType is the hardware acceleration method to use.
	// +optional
	// +kubebuilder:validation:Enum=none;amf;qsv;nvenc;v4l2m2m;vaapi;videotoolbox;rkmpp
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

	// qsvDevice is the Intel Quick Sync device, when there's more than one.
	// +optional
	QsvDevice string `json:"qsvDevice,omitempty"`

	// encodingThreadCount is how many threads ffmpeg uses. -1 picks one from the CPU count.
	// +optional
	// +kubebuilder:validation:Minimum=-1
	EncodingThreadCount *int `json:"encodingThreadCount,omitempty"`

	// enableDecodingColorDepth10Hevc decodes 10-bit HEVC in hardware.
	// +optional
	EnableDecodingColorDepth10Hevc *bool `json:"enableDecodingColorDepth10Hevc,omitempty"`

	// enableDecodingColorDepth10Vp9 decodes 10-bit VP9 in hardware.
	// +optional
	EnableDecodingColorDepth10Vp9 *bool `json:"enableDecodingColorDepth10Vp9,omitempty"`

	// enableDecodingColorDepth10HevcRext decodes 10-bit HEVC range extensions in hardware.
	// +optional
	EnableDecodingColorDepth10HevcRext *bool `json:"enableDecodingColorDepth10HevcRext,omitempty"`

	// enableDecodingColorDepth12HevcRext decodes 12-bit HEVC range extensions in hardware.
	// +optional
	EnableDecodingColorDepth12HevcRext *bool `json:"enableDecodingColorDepth12HevcRext,omitempty"`

	// preferSystemNativeHwDecoder uses the operating system's hardware decoder
	// where there is one.
	// +optional
	PreferSystemNativeHwDecoder *bool `json:"preferSystemNativeHwDecoder,omitempty"`

	// enableEnhancedNvdecDecoder uses NVIDIA's own decoder instead of CUVID.
	// +optional
	EnableEnhancedNvdecDecoder *bool `json:"enableEnhancedNvdecDecoder,omitempty"`

	// allowAv1Encoding allows AV1 hardware encoding.
	// +optional
	AllowAv1Encoding *bool `json:"allowAv1Encoding,omitempty"`

	// tonemappingAlgorithm is the algorithm for HDR to SDR tone mapping.
	// +optional
	// +kubebuilder:validation:Enum=none;clip;linear;gamma;reinhard;hable;mobius;bt2390
	TonemappingAlgorithm string `json:"tonemappingAlgorithm,omitempty"`

	// tonemappingMode is what tone mapping applies to.
	// +optional
	// +kubebuilder:validation:Enum=auto;max;rgb;lum;itp
	TonemappingMode string `json:"tonemappingMode,omitempty"`

	// tonemappingRange is the output color range of tone mapping.
	// +optional
	// +kubebuilder:validation:Enum=auto;tv;pc
	TonemappingRange string `json:"tonemappingRange,omitempty"`

	// tonemappingDesat desaturates highlights brighter than this level.
	// +optional
	TonemappingDesat *commonv1alpha1.Decimal `json:"tonemappingDesat,omitempty"`

	// tonemappingPeak is the signal peak to assume, in nits, when the video doesn't say.
	// +optional
	TonemappingPeak *commonv1alpha1.Decimal `json:"tonemappingPeak,omitempty"`

	// tonemappingParam tunes the tone mapping algorithm. 0 uses its default.
	// +optional
	TonemappingParam *commonv1alpha1.Decimal `json:"tonemappingParam,omitempty"`

	// vppTonemappingBrightness is the brightness gain of VPP tone mapping.
	// +optional
	VppTonemappingBrightness *commonv1alpha1.Decimal `json:"vppTonemappingBrightness,omitempty"`

	// vppTonemappingContrast is the contrast gain of VPP tone mapping.
	// +optional
	VppTonemappingContrast *commonv1alpha1.Decimal `json:"vppTonemappingContrast,omitempty"`

	// deinterlaceMethod is the deinterlacing filter.
	// +optional
	// +kubebuilder:validation:Enum=yadif;bwdif
	DeinterlaceMethod string `json:"deinterlaceMethod,omitempty"`

	// deinterlaceDoubleRate doubles the frame rate when deinterlacing.
	// +optional
	DeinterlaceDoubleRate *bool `json:"deinterlaceDoubleRate,omitempty"`

	// enableSegmentDeletion deletes transcoded segments once clients have them.
	// +optional
	EnableSegmentDeletion *bool `json:"enableSegmentDeletion,omitempty"`

	// segmentKeepSeconds is how many seconds of segments enableSegmentDeletion keeps.
	// +optional
	// +kubebuilder:validation:Minimum=0
	SegmentKeepSeconds *int `json:"segmentKeepSeconds,omitempty"`

	// enableAudioVbr encodes audio at a variable bitrate.
	// +optional
	EnableAudioVbr *bool `json:"enableAudioVbr,omitempty"`

	// downMixAudioBoost is the volume gain when downmixing to stereo.
	// +optional
	DownMixAudioBoost *commonv1alpha1.Decimal `json:"downMixAudioBoost,omitempty"`

	// downMixStereoAlgorithm is the algorithm for downmixing to stereo.
	// +optional
	// +kubebuilder:validation:Enum=None;Dave750;NightmodeDialogue;Rfc7845;Ac4
	DownMixStereoAlgorithm string `json:"downMixStereoAlgorithm,omitempty"`

	// maxMuxingQueueSize is the most packets ffmpeg buffers while muxing.
	// +optional
	// +kubebuilder:validation:Minimum=1
	MaxMuxingQueueSize *int `json:"maxMuxingQueueSize,omitempty"`

	// subtitleExtractionTimeoutMinutes is how long subtitle extraction may take.
	// +optional
	// +kubebuilder:validation:Minimum=1
	SubtitleExtractionTimeoutMinutes *int `json:"subtitleExtractionTimeoutMinutes,omitempty"`

	// allowOnDemandMetadataBasedKeyframeExtractionForExtensions lists the file
	// extensions, such as mkv, whose key frames Jellyfin reads from metadata
	// when it needs them.
	// +optional
	AllowOnDemandMetadataBasedKeyframeExtractionForExtensions []string `json:"allowOnDemandMetadataBasedKeyframeExtractionForExtensions,omitempty"`
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
func (c *JellyfinConfig) GetManagedResources() *map[string][]string {
	return &c.Status.ManagedResources
}
