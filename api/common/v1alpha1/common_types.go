// +kubebuilder:object:generate=true
package v1alpha1

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ReconcileConfig holds common reconciliation settings shared across all config CRDs.
type ReconcileConfig struct {
	// interval overrides the default reconciliation interval (default: 5m).
	// Uses standard Kubernetes duration format (e.g., "30s", "5m", "1h").
	// +optional
	Interval *metav1.Duration `json:"interval,omitempty"`

	// prune enables deletion of resources that exist in the app but are not declared in the CRD.
	// When true, for each resource type where at least one item is specified, any existing
	// resources not matching a declared item will be removed from the app.
	// Root folders and tags are never pruned.
	// Default: false (only create/update, never delete).
	// +optional
	Prune *bool `json:"prune,omitempty"`
}

// SecretKeyRef references a key within a Kubernetes Secret.
type SecretKeyRef struct {
	// name is the name of the Secret.
	// +required
	Name string `json:"name"`

	// key is the key within the Secret.
	// +required
	Key string `json:"key"`
}

// TLSConfig configures TLS for connections to an app's API.
// Use this when the target app uses HTTPS with a self-signed certificate or a private CA.
type TLSConfig struct {
	// insecureSkipVerify disables TLS certificate verification.
	// WARNING: This makes the connection susceptible to machine-in-the-middle attacks.
	// Only use for testing or when the target app uses a self-signed certificate and
	// providing a CA is not practical.
	// +optional
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty"`

	// caSecretRef references a Secret containing a PEM-encoded CA certificate bundle
	// used to verify the target app's TLS certificate.
	// The Secret must contain the CA certificate under the specified key.
	// +optional
	CASecretRef *SecretKeyRef `json:"caSecretRef,omitempty"`
}

// AppConnection defines how the operator connects to an app's API.
type AppConnection struct {
	// url is the base URL of the app (e.g., http://arr-sonarr.arr.svc.cluster.local:8989).
	// +required
	URL string `json:"url"`

	// apiKeySecretRef references a Secret containing the API key.
	// +required
	APIKeySecretRef SecretKeyRef `json:"apiKeySecretRef"`

	// tls configures TLS settings for the connection.
	// Only needed when the app uses HTTPS with self-signed or private CA certificates.
	// +optional
	TLS *TLSConfig `json:"tls,omitempty"`
}

// ConfigField represents a key-value field in a download client configuration.
type ConfigField struct {
	// name is the field name.
	// +required
	Name string `json:"name"`

	// value is the field value. Can be a string, number, or boolean.
	// +optional
	Value *FieldValue `json:"value,omitempty"`
}

// FieldValue wraps a value that can be a string, int, or bool.
// +kubebuilder:validation:Type=""
// +kubebuilder:pruning:PreserveUnknownFields
type FieldValue struct {
	Raw []byte `json:"-"`
}

// MarshalJSON returns the raw JSON encoding of the value.
func (v FieldValue) MarshalJSON() ([]byte, error) {
	if v.Raw == nil {
		return []byte("null"), nil
	}
	return v.Raw, nil
}

// UnmarshalJSON stores the raw JSON value.
func (v *FieldValue) UnmarshalJSON(data []byte) error {
	if data == nil || string(data) == "null" {
		v.Raw = nil
		return nil
	}
	v.Raw = append(v.Raw[:0:0], data...)
	return nil
}

// ToInterface returns the underlying Go value (string, float64, bool, or nil).
func (v FieldValue) ToInterface() interface{} {
	if v.Raw == nil {
		return nil
	}
	var val interface{}
	if err := json.Unmarshal(v.Raw, &val); err != nil {
		return string(v.Raw)
	}
	return val
}

// RootFolder defines a media root folder path.
type RootFolder struct {
	// path is the filesystem path for the root folder inside the container.
	// +required
	Path string `json:"path"`
}

// DownloadClient defines a download client connection.
type DownloadClient struct {
	// name is the display name of the download client.
	// +required
	Name string `json:"name"`

	// enable controls whether this download client is active.
	// +optional
	// +kubebuilder:default=true
	Enable *bool `json:"enable,omitempty"`

	// protocol is the download protocol.
	// +required
	// +kubebuilder:validation:Enum=torrent;usenet
	Protocol string `json:"protocol"`

	// implementation is the download client type (e.g., QBittorrent, Sabnzbd, NzbGet, Transmission, Deluge).
	// +required
	Implementation string `json:"implementation"`

	// configContract overrides the default config contract name.
	// Defaults to "<implementation>Settings" if not set.
	// +optional
	ConfigContract string `json:"configContract,omitempty"`

	// host is the hostname or IP of the download client.
	// +required
	Host string `json:"host"`

	// port is the port of the download client.
	// +required
	Port int `json:"port"`

	// useSsl enables SSL/TLS for the download client connection.
	// +optional
	UseSsl *bool `json:"useSsl,omitempty"`

	// urlBase is the base URL path for the download client (e.g., "/qbittorrent").
	// +optional
	UrlBase string `json:"urlBase,omitempty"`

	// category is the download category to use.
	// +optional
	Category string `json:"category,omitempty"`

	// priority is the download client priority. Lower values are higher priority.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=50
	// +kubebuilder:default=1
	Priority *int `json:"priority,omitempty"`

	// removeCompletedDownloads controls whether completed downloads are removed.
	// +optional
	// +kubebuilder:default=true
	RemoveCompletedDownloads *bool `json:"removeCompletedDownloads,omitempty"`

	// removeFailedDownloads controls whether failed downloads are removed.
	// +optional
	// +kubebuilder:default=true
	RemoveFailedDownloads *bool `json:"removeFailedDownloads,omitempty"`

	// usernameSecretRef references a Secret containing the username for authentication.
	// +optional
	UsernameSecretRef *SecretKeyRef `json:"usernameSecretRef,omitempty"`

	// passwordSecretRef references a Secret containing the password for authentication.
	// +optional
	PasswordSecretRef *SecretKeyRef `json:"passwordSecretRef,omitempty"`

	// apiKeySecretRef references a Secret containing the API key (for Usenet clients like SABnzbd/NzbGet).
	// +optional
	APIKeySecretRef *SecretKeyRef `json:"apiKeySecretRef,omitempty"`

	// tags is the list of tag IDs to associate with this download client.
	// +optional
	Tags []int `json:"tags,omitempty"`

	// fields defines additional implementation-specific configuration fields.
	// Use these for settings not covered by first-class fields (e.g., initialState, sequentialOrder).
	// +optional
	Fields []ConfigField `json:"fields,omitempty"`
}

// QualityProfile defines a quality profile that controls which qualities are acceptable
// and when upgrades should occur.
type QualityProfile struct {
	// name is the display name of the quality profile.
	// +required
	Name string `json:"name"`

	// upgradeAllowed controls whether upgrades to better qualities are allowed.
	// +optional
	UpgradeAllowed *bool `json:"upgradeAllowed,omitempty"`

	// cutoff is the quality or group name at which quality upgrades stop.
	// Must match a quality name or group name in the items list.
	// +optional
	Cutoff string `json:"cutoff,omitempty"`

	// minFormatScore is the minimum custom format score required to grab a release.
	// +optional
	MinFormatScore *int `json:"minFormatScore,omitempty"`

	// cutoffFormatScore is the custom format score at which upgrades stop.
	// +optional
	CutoffFormatScore *int `json:"cutoffFormatScore,omitempty"`

	// minUpgradeFormatScore is the minimum score improvement required for a format-based upgrade.
	// +optional
	MinUpgradeFormatScore *int `json:"minUpgradeFormatScore,omitempty"`

	// items defines the quality ordering and allowed states.
	// Items at the end of the list are higher priority. Each item is either
	// an individual quality (with quality set) or a quality group (with name and items set).
	// +optional
	Items []QualityProfileItem `json:"items,omitempty"`

	// formatItems assigns custom format scores within this profile.
	// Reference custom formats by name; IDs are resolved automatically.
	// +optional
	FormatItems []QualityProfileFormatItem `json:"formatItems,omitempty"`
}

// QualityProfileItem represents a quality or quality group in a profile.
// Set quality for an individual quality, or set name+items for a group.
type QualityProfileItem struct {
	// name is the group name. Set only for quality groups, not individual qualities.
	// +optional
	Name string `json:"name,omitempty"`

	// quality identifies a specific quality level.
	// Set for individual qualities, omit for groups.
	// +optional
	Quality *QualityReference `json:"quality,omitempty"`

	// allowed controls whether this quality or group is enabled in the profile.
	// +optional
	Allowed *bool `json:"allowed,omitempty"`

	// items contains the child qualities when this is a group.
	// Only set for quality groups.
	// +optional
	Items []QualityProfileGroupItem `json:"items,omitempty"`
}

// QualityProfileGroupItem represents an individual quality within a quality group.
type QualityProfileGroupItem struct {
	// quality identifies the quality level.
	// +required
	Quality QualityReference `json:"quality"`

	// allowed controls whether this quality is enabled.
	// +optional
	Allowed *bool `json:"allowed,omitempty"`
}

// QualityReference identifies a specific quality level by ID and name.
type QualityReference struct {
	// id is the quality ID (e.g., 7 for Bluray-1080p). IDs are app-specific.
	// +required
	ID int `json:"id"`

	// name is the quality display name (e.g., "Bluray-1080p").
	// +optional
	Name string `json:"name,omitempty"`
}

// QualityProfileFormatItem assigns a score to a custom format within a quality profile.
type QualityProfileFormatItem struct {
	// name is the custom format name. Must match an existing custom format.
	// +required
	Name string `json:"name"`

	// score is the score to assign. Positive values prefer, negative values penalize.
	// +required
	Score int `json:"score"`
}

// CustomFormat defines a custom format with matching specifications.
// Custom formats score releases based on matching conditions and are used
// within quality profiles to guide upgrade decisions.
type CustomFormat struct {
	// name is the display name of the custom format.
	// +required
	Name string `json:"name"`

	// includeCustomFormatWhenRenaming includes this format's name in renamed filenames
	// via the {Custom Formats} naming token.
	// +optional
	IncludeCustomFormatWhenRenaming *bool `json:"includeCustomFormatWhenRenaming,omitempty"`

	// specifications defines the matching conditions for this custom format.
	// A release matches if all required specs match and at least one non-required spec matches.
	// +optional
	Specifications []CustomFormatSpecification `json:"specifications,omitempty"`
}

// CustomFormatSpecification defines a single matching condition within a custom format.
type CustomFormatSpecification struct {
	// name is a human-readable label for this condition.
	// +required
	Name string `json:"name"`

	// implementation is the specification type.
	// Common values: ReleaseTitleSpecification, SourceSpecification, ResolutionSpecification,
	// QualityModifierSpecification, LanguageSpecification, ReleaseGroupSpecification,
	// EditionSpecification (Radarr), ReleaseTypeSpecification (Sonarr), SizeSpecification,
	// IndexerFlagSpecification.
	// +required
	Implementation string `json:"implementation"`

	// negate inverts the match logic. If true, the spec matches when the condition is NOT met.
	// +optional
	Negate *bool `json:"negate,omitempty"`

	// required makes this spec mandatory. If true, the custom format only matches when
	// this spec matches. If false, this spec participates in OR logic with other non-required specs.
	// +optional
	Required *bool `json:"required,omitempty"`

	// fields defines the specification's configuration values.
	// Most specs have a single field named "value" (e.g., a regex pattern or enum int).
	// SizeSpecification has "min" and "max" fields (floats in GB).
	// +optional
	Fields []ConfigField `json:"fields,omitempty"`
}

// Tag defines a tag that can be used to link resources together
// (download clients, indexers, notifications, etc.).
type Tag struct {
	// label is the tag name.
	// +required
	Label string `json:"label"`
}

// Indexer defines an indexer connection for a Servarr app.
type Indexer struct {
	// name is the display name of the indexer.
	// +required
	Name string `json:"name"`

	// enable controls whether this indexer is active.
	// +optional
	// +kubebuilder:default=true
	Enable *bool `json:"enable,omitempty"`

	// protocol is the indexer protocol.
	// +required
	// +kubebuilder:validation:Enum=torrent;usenet
	Protocol string `json:"protocol"`

	// implementation is the indexer type (e.g., Newznab, Torznab).
	// +required
	Implementation string `json:"implementation"`

	// configContract is the configuration contract name for the implementation.
	// Defaults to "<implementation>Settings" if not set.
	// +optional
	ConfigContract string `json:"configContract,omitempty"`

	// priority is the indexer priority. Lower values are higher priority.
	// +optional
	// +kubebuilder:validation:Minimum=1
	Priority *int `json:"priority,omitempty"`

	// enableRss enables RSS feed polling for this indexer.
	// +optional
	// +kubebuilder:default=true
	EnableRss *bool `json:"enableRss,omitempty"`

	// enableAutomaticSearch enables automatic search for this indexer.
	// +optional
	// +kubebuilder:default=true
	EnableAutomaticSearch *bool `json:"enableAutomaticSearch,omitempty"`

	// enableInteractiveSearch enables interactive (manual) search for this indexer.
	// +optional
	// +kubebuilder:default=true
	EnableInteractiveSearch *bool `json:"enableInteractiveSearch,omitempty"`

	// tags is the list of tag IDs to associate with this indexer.
	// +optional
	Tags []int `json:"tags,omitempty"`

	// fields defines the indexer's configuration fields (e.g., baseUrl, apiKey, categories).
	// +optional
	Fields []ConfigField `json:"fields,omitempty"`
}

// Notification defines a notification/connection for a Servarr app.
type Notification struct {
	// name is the display name of the notification.
	// +required
	Name string `json:"name"`

	// implementation is the notification type (e.g., Discord, Slack, Webhook, Email).
	// +required
	Implementation string `json:"implementation"`

	// configContract is the configuration contract name for the implementation.
	// Defaults to "<implementation>Settings" if not set.
	// +optional
	ConfigContract string `json:"configContract,omitempty"`

	// tags is the list of tag IDs to associate with this notification.
	// +optional
	Tags []int `json:"tags,omitempty"`

	// fields defines the notification's configuration fields (e.g., webhook URL, API token).
	// +optional
	Fields []ConfigField `json:"fields,omitempty"`

	// Trigger conditions — set to true to fire on these events.
	// Available triggers vary by app; unknown triggers are ignored.

	// onGrab fires when a release is grabbed (sent to download client).
	// +optional
	OnGrab *bool `json:"onGrab,omitempty"`

	// onDownload fires when a download is imported.
	// +optional
	OnDownload *bool `json:"onDownload,omitempty"`

	// onUpgrade fires when an existing file is upgraded.
	// +optional
	OnUpgrade *bool `json:"onUpgrade,omitempty"`

	// onRename fires when files are renamed.
	// +optional
	OnRename *bool `json:"onRename,omitempty"`

	// onHealthIssue fires when a health check fails.
	// +optional
	OnHealthIssue *bool `json:"onHealthIssue,omitempty"`

	// onHealthRestored fires when a health check recovers.
	// +optional
	OnHealthRestored *bool `json:"onHealthRestored,omitempty"`

	// onApplicationUpdate fires when the application is updated.
	// +optional
	OnApplicationUpdate *bool `json:"onApplicationUpdate,omitempty"`

	// onManualInteractionRequired fires when manual interaction is needed.
	// +optional
	OnManualInteractionRequired *bool `json:"onManualInteractionRequired,omitempty"`

	// includeHealthWarnings includes health warnings (not just errors) in notifications.
	// +optional
	IncludeHealthWarnings *bool `json:"includeHealthWarnings,omitempty"`

	// Sonarr-specific triggers

	// onSeriesAdd fires when a new series is added (Sonarr).
	// +optional
	OnSeriesAdd *bool `json:"onSeriesAdd,omitempty"`

	// onSeriesDelete fires when a series is deleted (Sonarr).
	// +optional
	OnSeriesDelete *bool `json:"onSeriesDelete,omitempty"`

	// onEpisodeFileDelete fires when an episode file is deleted (Sonarr).
	// +optional
	OnEpisodeFileDelete *bool `json:"onEpisodeFileDelete,omitempty"`

	// onEpisodeFileDeleteForUpgrade fires when an episode file is deleted for upgrade (Sonarr).
	// +optional
	OnEpisodeFileDeleteForUpgrade *bool `json:"onEpisodeFileDeleteForUpgrade,omitempty"`

	// Radarr-specific triggers

	// onMovieAdded fires when a new movie is added (Radarr).
	// +optional
	OnMovieAdded *bool `json:"onMovieAdded,omitempty"`

	// onMovieDelete fires when a movie is deleted (Radarr).
	// +optional
	OnMovieDelete *bool `json:"onMovieDelete,omitempty"`

	// onMovieFileDelete fires when a movie file is deleted (Radarr).
	// +optional
	OnMovieFileDelete *bool `json:"onMovieFileDelete,omitempty"`

	// onMovieFileDeleteForUpgrade fires when a movie file is deleted for upgrade (Radarr).
	// +optional
	OnMovieFileDeleteForUpgrade *bool `json:"onMovieFileDeleteForUpgrade,omitempty"`
}

// ImportList defines an import list for automatically adding content to a Servarr app.
type ImportList struct {
	// name is the display name of the import list.
	// +required
	Name string `json:"name"`

	// enable controls whether this import list is active.
	// +optional
	// +kubebuilder:default=true
	Enable *bool `json:"enable,omitempty"`

	// enableAutomaticAdd automatically adds items from this list.
	// +optional
	EnableAutomaticAdd *bool `json:"enableAutomaticAdd,omitempty"`

	// implementation is the import list type (e.g., TraktListImport, IMDbListImport, SonarrImport).
	// +required
	Implementation string `json:"implementation"`

	// configContract is the configuration contract name for the implementation.
	// Defaults to "<implementation>Settings" if not set.
	// +optional
	ConfigContract string `json:"configContract,omitempty"`

	// monitor controls how imported items are monitored.
	// Values vary by app (Sonarr: all, future, missing, existing, firstSeason, latestSeason, pilot, none;
	// Radarr: movieOnly, movieAndCollection, none).
	// +optional
	Monitor string `json:"monitor,omitempty"`

	// qualityProfileName is the name of the quality profile to assign to imported items.
	// Must match an existing quality profile. Resolved to an ID at reconciliation time.
	// +optional
	QualityProfileName string `json:"qualityProfileName,omitempty"`

	// rootFolderPath is the root folder path for imported items.
	// +optional
	RootFolderPath string `json:"rootFolderPath,omitempty"`

	// shouldMonitor controls whether imported items are monitored (Sonarr).
	// +optional
	ShouldMonitor *bool `json:"shouldMonitor,omitempty"`

	// seriesType is the series type for imported items (Sonarr: standard, daily, anime).
	// +optional
	SeriesType string `json:"seriesType,omitempty"`

	// seasonFolder enables season folders for imported items (Sonarr).
	// +optional
	SeasonFolder *bool `json:"seasonFolder,omitempty"`

	// minimumAvailability controls when imported movies are considered available (Radarr: announced, inCinemas, released).
	// +optional
	MinimumAvailability string `json:"minimumAvailability,omitempty"`

	// searchOnAdd searches for items immediately after adding (Radarr).
	// +optional
	SearchOnAdd *bool `json:"searchOnAdd,omitempty"`

	// listOrder controls the display order of this list.
	// +optional
	ListOrder *int `json:"listOrder,omitempty"`

	// tags is the list of tag IDs to associate with this import list.
	// +optional
	Tags []int `json:"tags,omitempty"`

	// fields defines the import list's configuration fields (e.g., API URL, access token, list ID).
	// +optional
	Fields []ConfigField `json:"fields,omitempty"`
}
