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

// AutobrrConfigSpec defines the desired state of AutobrrConfig.
type AutobrrConfigSpec struct {
	// connection defines how to connect to the Autobrr instance.
	// +required
	Connection commonv1alpha1.AppConnection `json:"connection"`

	// reconcile configures reconciliation behavior (e.g., sync interval).
	// +optional
	Reconcile *commonv1alpha1.ReconcileConfig `json:"reconcile,omitempty"`

	// downloadClients defines download client connections in Autobrr.
	// +optional
	DownloadClients []AutobrrDownloadClient `json:"downloadClients,omitempty"`

	// indexers defines indexer/tracker connections.
	// +optional
	Indexers []AutobrrIndexer `json:"indexers,omitempty"`

	// ircNetworks defines IRC network connections for announce channel monitoring.
	// +optional
	IRCNetworks []AutobrrIRCNetwork `json:"ircNetworks,omitempty"`

	// feeds defines RSS/Torznab feed sources.
	// +optional
	Feeds []AutobrrFeed `json:"feeds,omitempty"`

	// filters defines release filters with match criteria and actions.
	// +optional
	Filters []AutobrrFilter `json:"filters,omitempty"`
}

// AutobrrDownloadClient defines a download client connection in Autobrr.
type AutobrrDownloadClient struct {
	// name is the display name of the download client.
	// +required
	Name string `json:"name"`

	// enable controls whether this download client is active.
	// +optional
	// +kubebuilder:default=true
	Enable *bool `json:"enable,omitempty"`

	// type is the download client type. Deluge means Deluge 2 (Autobrr's DELUGE_V2).
	// +required
	// +kubebuilder:validation:Enum=qBittorrent;Deluge;Transmission;rTorrent;SABnzbd;Sonarr;Radarr;Lidarr;Readarr;Whisparr
	Type string `json:"type"`

	// host is the hostname or URL of the download client.
	// +required
	Host string `json:"host"`

	// port is the port of the download client.
	// +optional
	Port *int `json:"port,omitempty"`

	// tls enables TLS for the connection.
	// +optional
	TLS *bool `json:"tls,omitempty"`

	// usernameSecretRef references a Secret containing the username.
	// +optional
	UsernameSecretRef *commonv1alpha1.SecretKeyRef `json:"usernameSecretRef,omitempty"`

	// passwordSecretRef references a Secret containing the password.
	// +optional
	PasswordSecretRef *commonv1alpha1.SecretKeyRef `json:"passwordSecretRef,omitempty"`

	// apiKeySecretRef references a Secret containing the API key, for Sonarr, Radarr, Lidarr,
	// Readarr and SABnzbd clients.
	// +optional
	APIKeySecretRef *commonv1alpha1.SecretKeyRef `json:"apiKeySecretRef,omitempty"`

	// settings is deprecated and rejected: Autobrr has no client-level category,
	// save path or content layout. Set category and savePath on filter actions.
	// +optional
	Settings *AutobrrDownloadClientSettings `json:"settings,omitempty"`
}

// AutobrrDownloadClientSettings holds additional download client settings.
type AutobrrDownloadClientSettings struct {
	// category is the default download category/label.
	// +optional
	Category string `json:"category,omitempty"`

	// savePath overrides the default save path.
	// +optional
	SavePath string `json:"savePath,omitempty"`

	// contentLayout controls the content layout (Original, Subfolder, NoSubfolder).
	// +optional
	ContentLayout string `json:"contentLayout,omitempty"`

	// prioritizeProperRepacks prioritizes proper/repack releases.
	// +optional
	PrioritizeProperRepacks *bool `json:"prioritizeProperRepacks,omitempty"`
}

// AutobrrIndexer defines an indexer/tracker connection in Autobrr.
type AutobrrIndexer struct {
	// name is the display name of the indexer.
	// +required
	Name string `json:"name"`

	// enable controls whether this indexer is active.
	// +optional
	// +kubebuilder:default=true
	Enable *bool `json:"enable,omitempty"`

	// identifier is the Autobrr indexer definition, such as "ipt" or "btn", or
	// "torznab", "newznab" or "rss" for an indexer that a feed reads from.
	// +required
	Identifier string `json:"identifier"`

	// baseURL is the tracker's site URL. It defaults to the definition's first URL.
	// +optional
	BaseURL string `json:"baseURL,omitempty"`

	// settings are the definition's settings, such as passkey or rsskey.
	// +optional
	Settings []commonv1alpha1.ConfigField `json:"settings,omitempty"`
}

// AutobrrIRCNetwork defines an IRC network connection for announce monitoring.
type AutobrrIRCNetwork struct {
	// name is the display name of the IRC network.
	// +required
	Name string `json:"name"`

	// enable controls whether this IRC network connection is active.
	// +optional
	// +kubebuilder:default=true
	Enable *bool `json:"enable,omitempty"`

	// server is the IRC server address (e.g., "irc.example.com").
	// +required
	Server string `json:"server"`

	// port is the IRC server port.
	// +required
	Port int `json:"port"`

	// tls enables TLS for the IRC connection.
	// +optional
	TLS *bool `json:"tls,omitempty"`

	// nick is the IRC nickname to use.
	// +required
	Nick string `json:"nick"`

	// authMechanism is the authentication mechanism (SASL_PLAIN, NickServ, None).
	// +optional
	// +kubebuilder:validation:Enum=SASL_PLAIN;NickServ;None
	// +kubebuilder:default=None
	AuthMechanism string `json:"authMechanism,omitempty"`

	// authAccountSecretRef references a Secret containing the auth account name.
	// +optional
	AuthAccountSecretRef *commonv1alpha1.SecretKeyRef `json:"authAccountSecretRef,omitempty"`

	// authPasswordSecretRef references a Secret containing the auth password.
	// +optional
	AuthPasswordSecretRef *commonv1alpha1.SecretKeyRef `json:"authPasswordSecretRef,omitempty"`

	// inviteCommand is the IRC command to send to request channel invites.
	// +optional
	InviteCommand string `json:"inviteCommand,omitempty"`

	// channels defines the IRC channels to join.
	// +optional
	Channels []AutobrrIRCChannel `json:"channels,omitempty"`
}

// AutobrrIRCChannel defines an IRC channel to monitor.
type AutobrrIRCChannel struct {
	// name is the channel name (e.g., "#announce").
	// +required
	Name string `json:"name"`

	// passwordSecretRef references a Secret containing the channel password/key.
	// +optional
	PasswordSecretRef *commonv1alpha1.SecretKeyRef `json:"passwordSecretRef,omitempty"`
}

// AutobrrFeed defines an RSS or Torznab feed source.
type AutobrrFeed struct {
	// name is the display name of the feed.
	// +required
	Name string `json:"name"`

	// enable controls whether this feed is active.
	// +optional
	// +kubebuilder:default=true
	Enable *bool `json:"enable,omitempty"`

	// type is the feed type.
	// +required
	// +kubebuilder:validation:Enum=RSS;Torznab;Newznab
	Type string `json:"type"`

	// url is the feed URL.
	// +required
	URL string `json:"url"`

	// apiKeySecretRef references a Secret containing the feed API key (for Torznab/Newznab).
	// +optional
	APIKeySecretRef *commonv1alpha1.SecretKeyRef `json:"apiKeySecretRef,omitempty"`

	// interval is the feed polling interval in minutes.
	// +optional
	// +kubebuilder:default=15
	Interval *int `json:"interval,omitempty"`

	// indexerRef is the name of the indexer this feed belongs to: a torznab,
	// newznab or rss indexer declared under indexers.
	// +required
	IndexerRef string `json:"indexerRef"`
}

// AutobrrFilter defines a release filter with match criteria and actions.
type AutobrrFilter struct {
	// name is the display name of the filter.
	// +required
	Name string `json:"name"`

	// enable controls whether this filter is active.
	// +optional
	// +kubebuilder:default=true
	Enable *bool `json:"enable,omitempty"`

	// priority controls filter evaluation order. Lower values are evaluated first.
	// +optional
	Priority *int `json:"priority,omitempty"`

	// matchReleases is a comma-separated list of release name patterns to match (supports wildcards).
	// +optional
	MatchReleases string `json:"matchReleases,omitempty"`

	// exceptReleases is a comma-separated list of release name patterns to exclude.
	// +optional
	ExceptReleases string `json:"exceptReleases,omitempty"`

	// matchCategories is a comma-separated list of categories to match.
	// +optional
	MatchCategories string `json:"matchCategories,omitempty"`

	// resolutions is a list of acceptable resolutions.
	// +optional
	Resolutions []string `json:"resolutions,omitempty"`

	// sources is a list of acceptable sources (e.g., "BluRay", "WEB-DL").
	// +optional
	Sources []string `json:"sources,omitempty"`

	// codecs is a list of acceptable codecs (e.g., "x264", "x265", "HEVC").
	// +optional
	Codecs []string `json:"codecs,omitempty"`

	// minSize is the minimum release size (e.g., "100MB", "1GB").
	// +optional
	MinSize string `json:"minSize,omitempty"`

	// maxSize is the maximum release size (e.g., "50GB").
	// +optional
	MaxSize string `json:"maxSize,omitempty"`

	// delay is the delay in seconds before grabbing a matched release.
	// +optional
	Delay *int `json:"delay,omitempty"`

	// useRegex enables regex matching for release patterns.
	// +optional
	UseRegex *bool `json:"useRegex,omitempty"`

	// tags is a list of tags for organizational purposes.
	// +optional
	Tags []string `json:"tags,omitempty"`

	// indexers lists the indexers, by name, whose releases this filter checks.
	// Autobrr checks a filter only against the indexers it lists.
	// +optional
	Indexers []string `json:"indexers,omitempty"`

	// matchReleaseGroups is a comma-separated list of release groups to match.
	// +optional
	MatchReleaseGroups string `json:"matchReleaseGroups,omitempty"`

	// exceptReleaseGroups is a comma-separated list of release groups to skip.
	// +optional
	ExceptReleaseGroups string `json:"exceptReleaseGroups,omitempty"`

	// exceptCategories is a comma-separated list of categories to skip.
	// +optional
	ExceptCategories string `json:"exceptCategories,omitempty"`

	// matchUploaders is a comma-separated list of uploaders to match.
	// +optional
	MatchUploaders string `json:"matchUploaders,omitempty"`

	// exceptUploaders is a comma-separated list of uploaders to skip.
	// +optional
	ExceptUploaders string `json:"exceptUploaders,omitempty"`

	// matchReleaseTags is a comma-separated list of release tags to match.
	// +optional
	MatchReleaseTags string `json:"matchReleaseTags,omitempty"`

	// exceptReleaseTags is a comma-separated list of release tags to skip.
	// +optional
	ExceptReleaseTags string `json:"exceptReleaseTags,omitempty"`

	// useRegexReleaseTags reads the release tag patterns as regular expressions.
	// +optional
	UseRegexReleaseTags *bool `json:"useRegexReleaseTags,omitempty"`

	// matchDescription is a comma-separated list of description patterns to match.
	// +optional
	MatchDescription string `json:"matchDescription,omitempty"`

	// exceptDescription is a comma-separated list of description patterns to skip.
	// +optional
	ExceptDescription string `json:"exceptDescription,omitempty"`

	// useRegexDescription reads the description patterns as regular expressions.
	// +optional
	UseRegexDescription *bool `json:"useRegexDescription,omitempty"`

	// containers is a list of acceptable containers, such as mkv.
	// +optional
	Containers []string `json:"containers,omitempty"`

	// matchHdr is a list of HDR formats to match, such as DV or HDR10+.
	// +optional
	MatchHdr []string `json:"matchHdr,omitempty"`

	// exceptHdr is a list of HDR formats to skip.
	// +optional
	ExceptHdr []string `json:"exceptHdr,omitempty"`

	// matchOther is a list of other release attributes to match, such as REMUX.
	// +optional
	MatchOther []string `json:"matchOther,omitempty"`

	// exceptOther is a list of other release attributes to skip.
	// +optional
	ExceptOther []string `json:"exceptOther,omitempty"`

	// origins is a list of origins to match, such as P2P, INTERNAL or SCENE.
	// +optional
	Origins []string `json:"origins,omitempty"`

	// exceptOrigins is a list of origins to skip.
	// +optional
	ExceptOrigins []string `json:"exceptOrigins,omitempty"`

	// announceTypes is a list of announce types to match, such as NEW or PROMO.
	// +optional
	AnnounceTypes []string `json:"announceTypes,omitempty"`

	// matchLanguage is a list of languages to match.
	// +optional
	MatchLanguage []string `json:"matchLanguage,omitempty"`

	// exceptLanguage is a list of languages to skip.
	// +optional
	ExceptLanguage []string `json:"exceptLanguage,omitempty"`

	// shows is a comma-separated list of shows to match.
	// +optional
	Shows string `json:"shows,omitempty"`

	// seasons is a comma-separated list of seasons or ranges to match, such as 1,3-5.
	// +optional
	Seasons string `json:"seasons,omitempty"`

	// episodes is a comma-separated list of episodes or ranges to match.
	// +optional
	Episodes string `json:"episodes,omitempty"`

	// smartEpisode skips episodes older than ones already grabbed.
	// +optional
	SmartEpisode *bool `json:"smartEpisode,omitempty"`

	// years is a comma-separated list of years or ranges to match, such as 2018-2024.
	// +optional
	Years string `json:"years,omitempty"`

	// months is a comma-separated list of months to match.
	// +optional
	Months string `json:"months,omitempty"`

	// days is a comma-separated list of days to match.
	// +optional
	Days string `json:"days,omitempty"`

	// freeleech matches only freeleech releases.
	// +optional
	Freeleech *bool `json:"freeleech,omitempty"`

	// freeleechPercent is the freeleech percentages to match, such as 50%,75%-100%.
	// +optional
	FreeleechPercent string `json:"freeleechPercent,omitempty"`

	// maxDownloads caps how many releases the filter grabs per maxDownloadsUnit.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxDownloads *int `json:"maxDownloads,omitempty"`

	// maxDownloadsUnit is the period maxDownloads counts over.
	// +optional
	// +kubebuilder:validation:Enum=MINUTE;HOUR;DAY;WEEK;MONTH;EVER
	MaxDownloadsUnit string `json:"maxDownloadsUnit,omitempty"`

	// minSeeders is the fewest seeders a release may have.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MinSeeders *int `json:"minSeeders,omitempty"`

	// maxSeeders is the most seeders a release may have.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxSeeders *int `json:"maxSeeders,omitempty"`

	// minLeechers is the fewest leechers a release may have.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MinLeechers *int `json:"minLeechers,omitempty"`

	// maxLeechers is the most leechers a release may have.
	// +optional
	// +kubebuilder:validation:Minimum=0
	MaxLeechers *int `json:"maxLeechers,omitempty"`

	// exceptTags is a list of tags to skip.
	// +optional
	ExceptTags []string `json:"exceptTags,omitempty"`

	// tagsMatchLogic decides whether a release needs any or all of tags.
	// +optional
	// +kubebuilder:validation:Enum=ANY;ALL
	TagsMatchLogic string `json:"tagsMatchLogic,omitempty"`

	// exceptTagsMatchLogic decides whether any or all of exceptTags skip a release.
	// +optional
	// +kubebuilder:validation:Enum=ANY;ALL
	ExceptTagsMatchLogic string `json:"exceptTagsMatchLogic,omitempty"`

	// artists is a comma-separated list of artists to match.
	// +optional
	Artists string `json:"artists,omitempty"`

	// albums is a comma-separated list of albums to match.
	// +optional
	Albums string `json:"albums,omitempty"`

	// matchReleaseTypes is a list of music release types to match, such as Album or EP.
	// +optional
	MatchReleaseTypes []string `json:"matchReleaseTypes,omitempty"`

	// exceptReleaseTypes is a comma-separated list of music release types to skip.
	// +optional
	ExceptReleaseTypes string `json:"exceptReleaseTypes,omitempty"`

	// formats is a list of audio formats to match, such as FLAC or MP3.
	// +optional
	Formats []string `json:"formats,omitempty"`

	// quality is a list of audio qualities to match, such as Lossless or 320.
	// +optional
	Quality []string `json:"quality,omitempty"`

	// media is a list of music media to match, such as CD or WEB.
	// +optional
	Media []string `json:"media,omitempty"`

	// perfectFlac matches only lossless rips with a log of 100 and a cue.
	// +optional
	PerfectFlac *bool `json:"perfectFlac,omitempty"`

	// cue matches only releases with a cue file.
	// +optional
	Cue *bool `json:"cue,omitempty"`

	// log matches only releases with a rip log.
	// +optional
	Log *bool `json:"log,omitempty"`

	// logScore is the lowest rip log score to match.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=100
	LogScore *int `json:"logScore,omitempty"`

	// actions defines what to do when a release matches this filter.
	// +optional
	Actions []AutobrrFilterAction `json:"actions,omitempty"`
}

// AutobrrFilterAction defines an action to take when a filter matches.
type AutobrrFilterAction struct {
	// name is the display name of the action.
	// +required
	Name string `json:"name"`

	// enable controls whether this action is active.
	// +optional
	// +kubebuilder:default=true
	Enable *bool `json:"enable,omitempty"`

	// type is the action type. Deluge means Deluge 2 (Autobrr's DELUGE_V2).
	// +required
	// +kubebuilder:validation:Enum=qBittorrent;Deluge;Transmission;rTorrent;SABnzbd;Sonarr;Radarr;Lidarr;Readarr;Whisparr;Webhook;Exec;WatchFolder
	Type string `json:"type"`

	// clientRef is the name of the download client to use (must match a downloadClients entry).
	// +optional
	ClientRef string `json:"clientRef,omitempty"`

	// category overrides the download client's default category.
	// +optional
	Category string `json:"category,omitempty"`

	// savePath overrides the download client's default save path.
	// +optional
	SavePath string `json:"savePath,omitempty"`

	// webhookURL is the webhook URL (for Webhook action type).
	// +optional
	WebhookURL string `json:"webhookURL,omitempty"`

	// execCommand is the command to execute (for Exec action type).
	// +optional
	ExecCommand string `json:"execCommand,omitempty"`

	// execArgs is the arguments for the exec command.
	// +optional
	ExecArgs string `json:"execArgs,omitempty"`

	// watchFolder is the folder path (for WatchFolder action type).
	// +optional
	WatchFolder string `json:"watchFolder,omitempty"`

	// tags is a comma-separated list of tags the download client gives the torrent.
	// +optional
	Tags string `json:"tags,omitempty"`

	// label is the label the download client gives the torrent.
	// +optional
	Label string `json:"label,omitempty"`

	// downloadPath is where the download client keeps the torrent while it downloads.
	// +optional
	DownloadPath string `json:"downloadPath,omitempty"`

	// paused adds the torrent paused.
	// +optional
	Paused *bool `json:"paused,omitempty"`

	// ignoreRules sends the release even when the download client is past its limits.
	// +optional
	IgnoreRules *bool `json:"ignoreRules,omitempty"`

	// skipHashCheck adds the torrent without checking existing data.
	// +optional
	SkipHashCheck *bool `json:"skipHashCheck,omitempty"`

	// firstLastPiecePrio downloads the first and last pieces first.
	// +optional
	FirstLastPiecePrio *bool `json:"firstLastPiecePrio,omitempty"`

	// contentLayout decides whether the torrent's files go in a folder of their own.
	// +optional
	// +kubebuilder:validation:Enum=ORIGINAL;SUBFOLDER_CREATE;SUBFOLDER_NONE
	ContentLayout string `json:"contentLayout,omitempty"`

	// priority is where the torrent goes in the download client's queue.
	// +optional
	// +kubebuilder:validation:Enum=MAX;MIN;LOW;NORMAL;HIGH
	Priority string `json:"priority,omitempty"`

	// limitUploadSpeed caps the torrent's upload speed, in KiB/s.
	// +optional
	// +kubebuilder:validation:Minimum=0
	LimitUploadSpeed *int `json:"limitUploadSpeed,omitempty"`

	// limitDownloadSpeed caps the torrent's download speed, in KiB/s.
	// +optional
	// +kubebuilder:validation:Minimum=0
	LimitDownloadSpeed *int `json:"limitDownloadSpeed,omitempty"`

	// limitRatio is the share ratio at which the torrent stops seeding.
	// +optional
	LimitRatio *commonv1alpha1.Decimal `json:"limitRatio,omitempty"`

	// limitSeedTime is how many minutes the torrent seeds.
	// +optional
	// +kubebuilder:validation:Minimum=0
	LimitSeedTime *int `json:"limitSeedTime,omitempty"`

	// reannounceSkip adds the torrent without waiting for it to announce.
	// +optional
	ReannounceSkip *bool `json:"reannounceSkip,omitempty"`

	// reannounceDelete removes the torrent if it never announces.
	// +optional
	ReannounceDelete *bool `json:"reannounceDelete,omitempty"`

	// reannounceInterval is how many seconds to wait between reannounce attempts.
	// +optional
	// +kubebuilder:validation:Minimum=1
	ReannounceInterval *int `json:"reannounceInterval,omitempty"`

	// reannounceMaxAttempts is how many times to reannounce.
	// +optional
	// +kubebuilder:validation:Minimum=1
	ReannounceMaxAttempts *int `json:"reannounceMaxAttempts,omitempty"`

	// webhookType is the content type the webhook sends, such as JSON.
	// +optional
	WebhookType string `json:"webhookType,omitempty"`

	// webhookMethod is the HTTP method the webhook uses.
	// +optional
	// +kubebuilder:validation:Enum=GET;POST;PUT;PATCH;DELETE
	WebhookMethod string `json:"webhookMethod,omitempty"`

	// webhookData is the body the webhook sends; Autobrr fills in its macros.
	// +optional
	WebhookData string `json:"webhookData,omitempty"`

	// webhookHeaders are headers the webhook sends, as Name=Value.
	// +optional
	WebhookHeaders []string `json:"webhookHeaders,omitempty"`

	// externalDownloadClient is the download client, by name in Sonarr, Radarr, Lidarr or Readarr,
	// that the app sends the release to.
	// +optional
	ExternalDownloadClient string `json:"externalDownloadClient,omitempty"`
}

// AutobrrConfigStatus defines the observed state of AutobrrConfig.
type AutobrrConfigStatus struct {
	// conditions represent the current state of the AutobrrConfig resource.
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

// AutobrrConfig is the Schema for the autobrrconfigs API.
type AutobrrConfig struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec AutobrrConfigSpec `json:"spec"`

	// +optional
	Status AutobrrConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// AutobrrConfigList contains a list of AutobrrConfig.
type AutobrrConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []AutobrrConfig `json:"items"`
}

func (c *AutobrrConfig) GetConditions() *[]metav1.Condition                  { return &c.Status.Conditions }
func (c *AutobrrConfig) GetObservedGeneration() *int64                       { return &c.Status.ObservedGeneration }
func (c *AutobrrConfig) GetLastSyncTime() **metav1.Time                      { return &c.Status.LastSyncTime }
func (c *AutobrrConfig) GetReconcileConfig() *commonv1alpha1.ReconcileConfig { return c.Spec.Reconcile }
func (c *AutobrrConfig) GetManagedResources() *map[string][]string           { return &c.Status.ManagedResources }
