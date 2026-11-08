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

// SeerrConfigSpec defines the desired state of SeerrConfig.
// Exactly one of jellyfinAuth or plexAuth must be set.
type SeerrConfigSpec struct {
	// connection defines how to connect to the Seerr instance.
	// +required
	Connection SeerrConnection `json:"connection"`

	// reconcile configures reconciliation behavior (e.g., sync interval).
	// +optional
	Reconcile *commonv1alpha1.ReconcileConfig `json:"reconcile,omitempty"`

	// jellyfinAuth defines Jellyfin authentication settings.
	// Set this for Jellyfin-based Seerr instances. Mutually exclusive with plexAuth.
	// +optional
	JellyfinAuth *SeerrJellyfinAuth `json:"jellyfinAuth,omitempty"`

	// plexAuth defines Plex authentication settings.
	// Set this for Plex-based Seerr instances. Mutually exclusive with jellyfinAuth.
	// +optional
	PlexAuth *SeerrPlexAuth `json:"plexAuth,omitempty"`

	// main configures general application settings.
	// +optional
	Main *SeerrMain `json:"main,omitempty"`

	// sonarr lists the Sonarr servers Seerr sends TV requests to, matched by
	// name. Seerr uses the default server, and the default 4K server for 4K
	// requests.
	// +optional
	// +listType=map
	// +listMapKey=name
	Sonarr []SeerrServiceConnection `json:"sonarr,omitempty"`

	// radarr lists the Radarr servers Seerr sends movie requests to, matched by
	// name. Seerr uses the default server, and the default 4K server for 4K
	// requests.
	// +optional
	// +listType=map
	// +listMapKey=name
	Radarr []SeerrServiceConnection `json:"radarr,omitempty"`

	// notifications configures notification agents (Discord, Slack, Telegram, etc.).
	// Each agent is a singleton — only one configuration per agent type.
	// +optional
	Notifications []SeerrNotificationAgent `json:"notifications,omitempty"`
}

// SeerrNotificationAgent configures a single notification agent in Seerr.
type SeerrNotificationAgent struct {
	// agent is the notification agent type.
	// Supported values: discord, slack, telegram, pushover, email, webhook,
	// gotify, ntfy, pushbullet, webpush.
	// +required
	Agent string `json:"agent"`

	// enabled enables this notification agent.
	// +optional
	Enabled *bool `json:"enabled,omitempty"`

	// types is the notification type bitfield that controls which events trigger notifications.
	// Common values: 2=media requested, 4=media auto-approved, 8=media approved,
	// 16=media available, 32=media failed, 64=media declined, 128=test notification.
	// Combine with bitwise OR (e.g., 4094 for all types).
	// +optional
	Types *int `json:"types,omitempty"`

	// options sets the agent's settings, such as botUsername, chatId or
	// enableMentions. Values keep their type, so enableMentions: false is off.
	// +optional
	Options map[string]commonv1alpha1.FieldValue `json:"options,omitempty"`

	// secretOptions sets agent settings from Secrets, by setting name, such as
	// webhookUrl, botAPI or accessToken. They take precedence over options.
	// +optional
	SecretOptions map[string]commonv1alpha1.SecretKeyRef `json:"secretOptions,omitempty"`
}

// SeerrConnection defines how the operator connects to Seerr.
type SeerrConnection struct {
	// url is the base URL of the Seerr instance (e.g., http://jellyseerr.media.svc.cluster.local:5055).
	// +required
	URL string `json:"url"`

	// apiKeySecretRef references a Secret containing Seerr's API key. When set,
	// it is used instead of logging in through the media server.
	// +optional
	APIKeySecretRef *commonv1alpha1.SecretKeyRef `json:"apiKeySecretRef,omitempty"`

	// tls configures TLS settings for the connection.
	// Only needed when Seerr uses HTTPS with self-signed or private CA certificates.
	// +optional
	TLS *commonv1alpha1.TLSConfig `json:"tls,omitempty"`
}

// SeerrPlexAuth defines Plex authentication for Seerr setup.
type SeerrPlexAuth struct {
	// hostname is the Plex server hostname.
	// +required
	Hostname string `json:"hostname"`

	// port is the Plex server port.
	// +optional
	// +kubebuilder:default=32400
	Port *int `json:"port,omitempty"`

	// useSsl enables SSL for the Plex connection.
	// +optional
	UseSsl *bool `json:"useSsl,omitempty"`

	// tokenSecretRef references a Secret containing the Plex auth token.
	// +required
	TokenSecretRef commonv1alpha1.SecretKeyRef `json:"tokenSecretRef"`
}

// SeerrJellyfinAuth defines the Jellyfin authentication configuration for Seerr.
type SeerrJellyfinAuth struct {
	// hostname is the Jellyfin server hostname.
	// +required
	Hostname string `json:"hostname"`

	// port is the Jellyfin server port.
	// +required
	Port *int `json:"port"`

	// useSsl enables SSL for the Jellyfin connection.
	// +optional
	UseSsl *bool `json:"useSsl,omitempty"`

	// urlBase is the subpath Jellyfin is served under, if any.
	// +optional
	UrlBase string `json:"urlBase,omitempty"`

	// usernameSecretRef references a Secret containing the Jellyfin username.
	// +required
	UsernameSecretRef commonv1alpha1.SecretKeyRef `json:"usernameSecretRef"`

	// passwordSecretRef references a Secret containing the Jellyfin password.
	// +required
	PasswordSecretRef commonv1alpha1.SecretKeyRef `json:"passwordSecretRef"`
}

// SeerrMain configures general Seerr application settings.
type SeerrMain struct {
	// applicationTitle is the application display title.
	// +optional
	ApplicationTitle string `json:"applicationTitle,omitempty"`

	// applicationUrl is the external URL for the application.
	// +optional
	ApplicationUrl string `json:"applicationUrl,omitempty"`

	// hideAvailable hides already-available media from discovery.
	// +optional
	HideAvailable *bool `json:"hideAvailable,omitempty"`

	// localLogin enables local user login.
	// +optional
	LocalLogin *bool `json:"localLogin,omitempty"`

	// mediaServerLogin enables login via the media server (Jellyfin).
	// +optional
	MediaServerLogin *bool `json:"mediaServerLogin,omitempty"`

	// defaultPermissions is the default permission bitmask for new users.
	// +optional
	DefaultPermissions *int `json:"defaultPermissions,omitempty"`

	// partialRequestsEnabled enables partial season/episode requests.
	// +optional
	PartialRequestsEnabled *bool `json:"partialRequestsEnabled,omitempty"`

	// locale is the application locale (e.g., "en").
	// +optional
	Locale string `json:"locale,omitempty"`
}

// SeerrServiceConnection defines a connection to a Sonarr or Radarr instance.
type SeerrServiceConnection struct {
	// name is the display name for this service connection.
	// +required
	Name string `json:"name"`

	// hostname is the service hostname.
	// +required
	Hostname string `json:"hostname"`

	// port is the service port.
	// +required
	Port *int `json:"port"`

	// apiKeySecretRef references a Secret containing the service API key.
	// +required
	APIKeySecretRef commonv1alpha1.SecretKeyRef `json:"apiKeySecretRef"`

	// useSsl enables SSL for the service connection.
	// +optional
	UseSsl *bool `json:"useSsl,omitempty"`

	// baseUrl is the URL base path for the service.
	// +optional
	BaseUrl string `json:"baseUrl,omitempty"`

	// activeProfileId is used when activeProfileName is omitted.
	// +optional
	ActiveProfileId *int `json:"activeProfileId,omitempty"`

	// activeProfileName is resolved against the service on each reconciliation.
	// When set, it takes precedence over activeProfileId; exactly one match is required.
	// +optional
	ActiveProfileName string `json:"activeProfileName,omitempty"`

	// activeDirectory is the root folder path for media.
	// +optional
	ActiveDirectory string `json:"activeDirectory,omitempty"`

	// is4k indicates this is a 4K instance.
	// +optional
	Is4k *bool `json:"is4k,omitempty"`

	// isDefault indicates this is the default service for its type.
	// +optional
	IsDefault *bool `json:"isDefault,omitempty"`

	// seriesType is the series type for Sonarr (e.g., "standard", "anime").
	// Only applicable when used with Sonarr.
	// +optional
	SeriesType string `json:"seriesType,omitempty"`

	// minimumAvailability is the minimum availability for Radarr.
	// Only applicable when used with Radarr.
	// +optional
	// +kubebuilder:validation:Enum=announced;inCinemas;released
	MinimumAvailability string `json:"minimumAvailability,omitempty"`

	// enableSeasonFolders enables season folder organization in Sonarr.
	// Only applicable when used with Sonarr.
	// +optional
	EnableSeasonFolders *bool `json:"enableSeasonFolders,omitempty"`

	// externalUrl is the address users open the server at, for links in Seerr.
	// +optional
	ExternalUrl string `json:"externalUrl,omitempty"`

	// syncEnabled scans the server for media and request status.
	// +optional
	SyncEnabled *bool `json:"syncEnabled,omitempty"`

	// preventSearch stops the server from searching as soon as a request is approved.
	// +optional
	PreventSearch *bool `json:"preventSearch,omitempty"`

	// tagRequests tags each request with the requesting user.
	// +optional
	TagRequests *bool `json:"tagRequests,omitempty"`

	// activeAnimeProfileName is the quality profile for anime, resolved against
	// the server on each reconciliation. Only applicable when used with Sonarr.
	// +optional
	ActiveAnimeProfileName string `json:"activeAnimeProfileName,omitempty"`

	// activeAnimeDirectory is the root folder for anime. Only applicable when
	// used with Sonarr.
	// +optional
	ActiveAnimeDirectory string `json:"activeAnimeDirectory,omitempty"`

	// animeSeriesType is the series type for anime. Only applicable when used
	// with Sonarr.
	// +optional
	// +kubebuilder:validation:Enum=standard;daily;anime
	AnimeSeriesType string `json:"animeSeriesType,omitempty"`

	// monitorNewItems decides whether seasons added to a series later are
	// monitored. Only applicable when used with Sonarr.
	// +optional
	// +kubebuilder:validation:Enum=all;none
	MonitorNewItems string `json:"monitorNewItems,omitempty"`
}

// SeerrConfigStatus defines the observed state of SeerrConfig.
type SeerrConfigStatus struct {
	// conditions represent the current state of the SeerrConfig resource.
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

	// initialized indicates whether the Seerr initial setup has been completed.
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

// SeerrConfig is the Schema for the seerrconfigs API.
type SeerrConfig struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec SeerrConfigSpec `json:"spec"`

	// +optional
	Status SeerrConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SeerrConfigList contains a list of SeerrConfig.
type SeerrConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SeerrConfig `json:"items"`
}

func (c *SeerrConfig) GetConditions() *[]metav1.Condition                  { return &c.Status.Conditions }
func (c *SeerrConfig) GetObservedGeneration() *int64                       { return &c.Status.ObservedGeneration }
func (c *SeerrConfig) GetLastSyncTime() **metav1.Time                      { return &c.Status.LastSyncTime }
func (c *SeerrConfig) GetReconcileConfig() *commonv1alpha1.ReconcileConfig { return c.Spec.Reconcile }
func (c *SeerrConfig) GetManagedResources() *map[string][]string           { return &c.Status.ManagedResources }
