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

	// type is the download client type.
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

	// apiKeySecretRef references a Secret containing the API key (for *arr apps, SABnzbd).
	// +optional
	APIKeySecretRef *commonv1alpha1.SecretKeyRef `json:"apiKeySecretRef,omitempty"`

	// settings contains additional client-specific settings.
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

	// implementation is the indexer implementation name (e.g., "ipt", "btn", "ptp").
	// This must match a known Autobrr indexer definition.
	// +required
	Implementation string `json:"implementation"`

	// baseURL is the indexer's base URL.
	// +optional
	BaseURL string `json:"baseURL,omitempty"`

	// apiKeySecretRef references a Secret containing the indexer API key or passkey.
	// +optional
	APIKeySecretRef *commonv1alpha1.SecretKeyRef `json:"apiKeySecretRef,omitempty"`

	// feedURL is the RSS/Torznab feed URL for this indexer.
	// +optional
	FeedURL string `json:"feedURL,omitempty"`
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

	// indexerRef is the name of the indexer this feed belongs to.
	// +optional
	IndexerRef string `json:"indexerRef,omitempty"`
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

	// indexers is a list of indexer names this filter applies to. Empty means all.
	// +optional
	Indexers []string `json:"indexers,omitempty"`

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

	// type is the action type.
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

func init() {
	SchemeBuilder.Register(&AutobrrConfig{}, &AutobrrConfigList{})
}
