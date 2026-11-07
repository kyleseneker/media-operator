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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
)

// MaintainerrConfigSpec defines the desired state of MaintainerrConfig.
type MaintainerrConfigSpec struct {
	// connection defines how to connect to the Maintainerr instance.
	// +required
	Connection MaintainerrConnection `json:"connection"`

	// reconcile configures reconciliation behavior (e.g., sync interval).
	// +optional
	Reconcile *commonv1alpha1.ReconcileConfig `json:"reconcile,omitempty"`

	// mediaServer connects Maintainerr to Plex or Jellyfin. The operator sets the
	// media server on a fresh Maintainerr only: switching an existing one deletes
	// every collection, so a different server already in use is reported as an
	// error instead.
	// +optional
	MediaServer *MaintainerrMediaServer `json:"mediaServer,omitempty"`

	// sonarr lists the Sonarr servers rule groups can act through.
	// +optional
	// +listType=map
	// +listMapKey=name
	Sonarr []MaintainerrArrServer `json:"sonarr,omitempty"`

	// radarr lists the Radarr servers rule groups can act through.
	// +optional
	// +listType=map
	// +listMapKey=name
	Radarr []MaintainerrArrServer `json:"radarr,omitempty"`

	// seerr connects Maintainerr to Seerr (or Overseerr or Jellyseerr), which
	// rules can read and which Maintainerr clears requests in.
	// +optional
	Seerr *MaintainerrServiceConnection `json:"seerr,omitempty"`

	// tautulli connects Maintainerr to Tautulli. Rules can read it only when
	// Plex is the media server.
	// +optional
	Tautulli *MaintainerrServiceConnection `json:"tautulli,omitempty"`

	// jobs sets when Maintainerr runs its rules and acts on its collections.
	// +optional
	Jobs *MaintainerrJobs `json:"jobs,omitempty"`

	// ruleGroups are the rules that put media into a collection and the action
	// Maintainerr takes on it. Each group is matched by name.
	// +optional
	// +listType=map
	// +listMapKey=name
	RuleGroups []MaintainerrRuleGroup `json:"ruleGroups,omitempty"`
}

// MaintainerrConnection defines how the operator reaches Maintainerr. Maintainerr
// has no API authentication; restrict access to it at the network level.
type MaintainerrConnection struct {
	// url is the base URL of Maintainerr, including any BASE_PATH
	// (e.g., http://maintainerr:6246).
	// +required
	URL string `json:"url"`

	// tls configures TLS settings for the connection.
	// Only needed when Maintainerr uses HTTPS with self-signed or private CA certificates.
	// +optional
	TLS *commonv1alpha1.TLSConfig `json:"tls,omitempty"`
}

// MaintainerrMediaServer selects the media server Maintainerr manages.
// +kubebuilder:validation:XValidation:rule="has(self.plex) != has(self.jellyfin)",message="set exactly one of plex or jellyfin"
type MaintainerrMediaServer struct {
	// plex connects Maintainerr to a Plex server.
	// +optional
	Plex *MaintainerrPlex `json:"plex,omitempty"`

	// jellyfin connects Maintainerr to a Jellyfin server. Maintainerr tests the
	// connection before saving it, so Jellyfin must be reachable from Maintainerr.
	// +optional
	Jellyfin *MaintainerrJellyfin `json:"jellyfin,omitempty"`
}

// MaintainerrPlex connects Maintainerr to a Plex server. The operator turns on
// Maintainerr's manual mode, so Maintainerr does not replace these values with
// another address it discovers through plex.tv.
type MaintainerrPlex struct {
	// hostname is the Plex host as Maintainerr reaches it, without a scheme
	// (e.g., plex.media.svc.cluster.local).
	// +required
	// +kubebuilder:validation:MinLength=1
	Hostname string `json:"hostname"`

	// port is the Plex port.
	// +optional
	// +kubebuilder:default=32400
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	Port int32 `json:"port,omitempty"`

	// ssl connects to Plex over HTTPS.
	// +optional
	SSL *bool `json:"ssl,omitempty"`

	// tokenSecretRef references a Secret containing a Plex token for an account
	// with access to the server.
	// +required
	TokenSecretRef commonv1alpha1.SecretKeyRef `json:"tokenSecretRef"`
}

// MaintainerrJellyfin connects Maintainerr to a Jellyfin server.
type MaintainerrJellyfin struct {
	// url is the Jellyfin URL as Maintainerr reaches it (e.g., http://jellyfin:8096).
	// +required
	// +kubebuilder:validation:Pattern=`^https?://`
	URL string `json:"url"`

	// apiKeySecretRef references a Secret containing a Jellyfin API key.
	// +required
	APIKeySecretRef commonv1alpha1.SecretKeyRef `json:"apiKeySecretRef"`

	// userId is the Jellyfin administrator Maintainerr acts as. Maintainerr
	// picks the first administrator when it is unset.
	// +optional
	UserID string `json:"userId,omitempty"`
}

// MaintainerrArrServer is a Sonarr or Radarr server in Maintainerr.
type MaintainerrArrServer struct {
	// name identifies the server here and in ruleGroups[].sonarrServer and
	// ruleGroups[].radarrServer. It is the server name Maintainerr shows.
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// url is the server URL as Maintainerr reaches it (e.g., http://radarr:7878).
	// +required
	// +kubebuilder:validation:Pattern=`^https?://`
	URL string `json:"url"`

	// apiKeySecretRef references a Secret containing the server's API key.
	// +required
	APIKeySecretRef commonv1alpha1.SecretKeyRef `json:"apiKeySecretRef"`

	// tagExclusions excludes media carrying exclusionTag in this server from
	// every rule group.
	// +optional
	TagExclusions *bool `json:"tagExclusions,omitempty"`

	// exclusionTag is the tag tagExclusions looks for. Maintainerr uses "dnd"
	// when it is unset.
	// +optional
	// +kubebuilder:validation:Pattern=`^[a-z0-9-]+$`
	ExclusionTag string `json:"exclusionTag,omitempty"`

	// untagOnUnexclude removes exclusionTag from media when it is no longer
	// excluded in Maintainerr.
	// +optional
	UntagOnUnexclude *bool `json:"untagOnUnexclude,omitempty"`
}

// MaintainerrServiceConnection connects Maintainerr to a service by URL and API key.
type MaintainerrServiceConnection struct {
	// url is the service URL as Maintainerr reaches it.
	// +required
	// +kubebuilder:validation:Pattern=`^https?://`
	URL string `json:"url"`

	// apiKeySecretRef references a Secret containing the service's API key.
	// +required
	APIKeySecretRef commonv1alpha1.SecretKeyRef `json:"apiKeySecretRef"`
}

// MaintainerrJobs sets Maintainerr's job schedules as cron expressions.
type MaintainerrJobs struct {
	// rules is when Maintainerr evaluates rule groups (e.g., "0 0-23/8 * * *").
	// +optional
	Rules string `json:"rules,omitempty"`

	// collections is when Maintainerr acts on media whose time in a collection
	// has run out (e.g., "0 0-23/12 * * *").
	// +optional
	Collections string `json:"collections,omitempty"`
}

// MaintainerrRuleGroup is a set of rules, the collection the matching media goes
// into and what Maintainerr does with it.
type MaintainerrRuleGroup struct {
	// name identifies the rule group and names its collection.
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// description is shown on the collection.
	// +optional
	Description string `json:"description,omitempty"`

	// library is the title of the media server library the rules run against.
	// +required
	// +kubebuilder:validation:MinLength=1
	Library string `json:"library"`

	// dataType is what a show library's rules match: whole shows, seasons or
	// episodes. Movie libraries always match movies. Changing it empties the
	// collection.
	// +optional
	// +kubebuilder:validation:Enum=movie;show;season;episode
	DataType string `json:"dataType,omitempty"`

	// active runs the rule group. Inactive groups keep their collection but are
	// not evaluated or acted on.
	// +optional
	// +kubebuilder:default=true
	Active *bool `json:"active,omitempty"`

	// arrAction is what Maintainerr does with media once actionAfterDays have
	// passed. Without a Sonarr or Radarr server, deleting actions delete through
	// the media server. changeQualityProfile needs qualityProfile.
	// +required
	// +kubebuilder:validation:Enum=delete;unmonitorDeleteAll;unmonitorDeleteExisting;unmonitor;doNothing;deleteShowIfEmpty;unmonitorShowIfEmpty;changeQualityProfile
	ArrAction string `json:"arrAction"`

	// actionAfterDays is how long media stays in the collection before arrAction
	// runs. A new group without it fills its collection but never acts. Removing
	// it later leaves Maintainerr's value in place: Maintainerr cannot clear it.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=36500
	ActionAfterDays *int32 `json:"actionAfterDays,omitempty"`

	// radarrServer names the spec.radarr server to act through. Required when a
	// rule reads Radarr.
	// +optional
	RadarrServer string `json:"radarrServer,omitempty"`

	// sonarrServer names the spec.sonarr server to act through. Required when a
	// rule reads Sonarr.
	// +optional
	SonarrServer string `json:"sonarrServer,omitempty"`

	// qualityProfile is the name of the Sonarr or Radarr quality profile that
	// changeQualityProfile switches media to.
	// +optional
	QualityProfile string `json:"qualityProfile,omitempty"`

	// listExclusions adds deleted media to the Sonarr or Radarr import list
	// exclusions, so lists do not add it again.
	// +optional
	ListExclusions *bool `json:"listExclusions,omitempty"`

	// cleanupLeftoverFolders deletes folders a deleting action leaves behind.
	// Maintainerr supports it for unmonitorDeleteAll on movies and shows,
	// unmonitorDeleteExisting on shows and seasons, and delete or
	// deleteShowIfEmpty on seasons.
	// +optional
	CleanupLeftoverFolders *bool `json:"cleanupLeftoverFolders,omitempty"`

	// forceSeerr removes the media's requests from Seerr when it is acted on,
	// even if Seerr is not otherwise involved. Not supported for episodes.
	// +optional
	ForceSeerr *bool `json:"forceSeerr,omitempty"`

	// tagInArr tags collection members in Sonarr or Radarr.
	// +optional
	TagInArr *bool `json:"tagInArr,omitempty"`

	// keepInMaintainerrOnly keeps the collection in Maintainerr without creating
	// it on the media server.
	// +optional
	KeepInMaintainerrOnly *bool `json:"keepInMaintainerrOnly,omitempty"`

	// visibleOnHome shows the collection on the media server's home screen.
	// +optional
	VisibleOnHome *bool `json:"visibleOnHome,omitempty"`

	// visibleOnRecommended shows the collection in the library's recommendations.
	// +optional
	VisibleOnRecommended *bool `json:"visibleOnRecommended,omitempty"`

	// schedule is a cron expression for this group's rule runs. Unset, the group
	// runs on spec.jobs.rules.
	// +optional
	Schedule string `json:"schedule,omitempty"`

	// sections are evaluated in order. Rules within a section combine with each
	// other first; each section then combines with the result of the ones before
	// it.
	// +required
	// +kubebuilder:validation:MinItems=1
	Sections []MaintainerrRuleSection `json:"sections"`
}

// MaintainerrRuleSection is a group of rules evaluated together.
type MaintainerrRuleSection struct {
	// operator combines this section with the sections before it. Required on
	// every section but the first, and not allowed on the first.
	// +optional
	// +kubebuilder:validation:Enum=AND;OR
	Operator string `json:"operator,omitempty"`

	// rules are combined in order, each with the result of the ones before it.
	// +required
	// +kubebuilder:validation:MinItems=1
	Rules []MaintainerrRule `json:"rules"`
}

// MaintainerrRule compares a property of the media with a value or with another
// property. It uses the names Maintainerr's rule YAML export uses.
type MaintainerrRule struct {
	// operator combines this rule with the rules before it in the section.
	// Required on every rule but a section's first, and not allowed on the first.
	// +optional
	// +kubebuilder:validation:Enum=AND;OR
	Operator string `json:"operator,omitempty"`

	// firstValue is the property compared, as Application.property (e.g.,
	// Plex.addDate, Jellyfin.viewCount, Radarr.fileSize). Plex and Jellyfin
	// properties must belong to the configured media server. A name Maintainerr
	// lists twice resolves to the one not marked deprecated.
	// +required
	// +kubebuilder:validation:Pattern=`^[A-Za-z]+\.[A-Za-z0-9_]+$`
	FirstValue string `json:"firstValue"`

	// action is the comparison.
	// +required
	// +kubebuilder:validation:Enum=BIGGER;SMALLER;EQUALS;NOT_EQUALS;CONTAINS;BEFORE;AFTER;IN_LAST;IN_NEXT;NOT_CONTAINS;CONTAINS_PARTIAL;NOT_CONTAINS_PARTIAL;CONTAINS_ALL;NOT_CONTAINS_ALL;COUNT_EQUALS;COUNT_NOT_EQUALS;COUNT_BIGGER;COUNT_SMALLER;EXISTS;NOT_EXISTS
	Action string `json:"action"`

	// lastValue is a second property to compare against, as Application.property.
	// Set lastValue or customValue, not both.
	// +optional
	// +kubebuilder:validation:Pattern=`^[A-Za-z]+\.[A-Za-z0-9_]+$`
	LastValue string `json:"lastValue,omitempty"`

	// customValue is a literal value to compare against.
	// +optional
	CustomValue *MaintainerrCustomValue `json:"customValue,omitempty"`

	// arrDiskPath limits disk-space properties to one Sonarr or Radarr root folder.
	// +optional
	ArrDiskPath string `json:"arrDiskPath,omitempty"`

	// username is the media server user that per-user properties (viewCountByUser,
	// watchTimeByUser, lastViewedAtByUser) are read for. Required by those
	// properties and not allowed on others.
	// +optional
	Username string `json:"username,omitempty"`
}

// MaintainerrCustomValue is a literal rule value.
type MaintainerrCustomValue struct {
	// type is how value is read. custom_days is a number of days, which date
	// properties compare relative to now. date takes an ISO date. boolean takes
	// "true" or "false". text_list takes a JSON array or a single value.
	// +required
	// +kubebuilder:validation:Enum=number;custom_days;date;text;text_list;boolean
	Type string `json:"type"`

	// value is the literal. Quote booleans: value: "true".
	// +required
	Value intstr.IntOrString `json:"value"`
}

// MaintainerrConfigStatus defines the observed state of MaintainerrConfig.
type MaintainerrConfigStatus struct {
	// conditions represent the current state of the MaintainerrConfig resource.
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

// MaintainerrConfig is the Schema for the maintainerrconfigs API.
type MaintainerrConfig struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec MaintainerrConfigSpec `json:"spec"`

	// +optional
	Status MaintainerrConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// MaintainerrConfigList contains a list of MaintainerrConfig.
type MaintainerrConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []MaintainerrConfig `json:"items"`
}

func (c *MaintainerrConfig) GetConditions() *[]metav1.Condition { return &c.Status.Conditions }
func (c *MaintainerrConfig) GetObservedGeneration() *int64      { return &c.Status.ObservedGeneration }
func (c *MaintainerrConfig) GetLastSyncTime() **metav1.Time     { return &c.Status.LastSyncTime }
func (c *MaintainerrConfig) GetReconcileConfig() *commonv1alpha1.ReconcileConfig {
	return c.Spec.Reconcile
}
func (c *MaintainerrConfig) GetManagedResources() *map[string][]string {
	return &c.Status.ManagedResources
}
