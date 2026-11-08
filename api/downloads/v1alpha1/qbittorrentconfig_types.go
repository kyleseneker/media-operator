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

// QBittorrentConfigSpec defines the desired state of QBittorrentConfig.
type QBittorrentConfigSpec struct {
	// connection defines how to connect to the qBittorrent instance.
	// +required
	Connection QBittorrentConnection `json:"connection"`

	// reconcile configures reconciliation behavior (e.g., sync interval).
	// +optional
	Reconcile *commonv1alpha1.ReconcileConfig `json:"reconcile,omitempty"`

	// preferences configures qBittorrent application preferences.
	// +optional
	Preferences *QBittorrentPreferences `json:"preferences,omitempty"`

	// categories defines download categories and their save paths.
	// +optional
	Categories []QBittorrentCategory `json:"categories,omitempty"`
}

// QBittorrentConnection defines how the operator connects to qBittorrent.
// Unlike AppConnection, qBittorrent uses username/password authentication.
type QBittorrentConnection struct {
	// url is the base URL of the qBittorrent Web UI (e.g., http://qbittorrent.media.svc.cluster.local:8080).
	// +required
	URL string `json:"url"`

	// usernameSecretRef references a Secret containing the username.
	// +required
	UsernameSecretRef commonv1alpha1.SecretKeyRef `json:"usernameSecretRef"`

	// passwordSecretRef references a Secret containing the password.
	// +required
	PasswordSecretRef commonv1alpha1.SecretKeyRef `json:"passwordSecretRef"`

	// tls configures TLS settings for the connection.
	// Only needed when qBittorrent uses HTTPS with self-signed or private CA certificates.
	// +optional
	TLS *commonv1alpha1.TLSConfig `json:"tls,omitempty"`
}

// QBittorrentPreferences configures qBittorrent application preferences.
type QBittorrentPreferences struct {
	// savePath is the default save path for downloads.
	// +optional
	SavePath string `json:"savePath,omitempty"`

	// tempPathEnabled enables using a temporary path for incomplete downloads.
	// +optional
	TempPathEnabled *bool `json:"tempPathEnabled,omitempty"`

	// dht enables the DHT (Distributed Hash Table) network.
	// +optional
	DHT *bool `json:"dht,omitempty"`

	// pex enables Peer Exchange.
	// +optional
	PEX *bool `json:"pex,omitempty"`

	// lsd enables Local Service Discovery.
	// +optional
	LSD *bool `json:"lsd,omitempty"`

	// encryption controls the encryption mode. 0=prefer, 1=force on, 2=force off.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=2
	Encryption *int `json:"encryption,omitempty"`

	// maxConnec is the global maximum number of connections. -1 means unlimited.
	// +optional
	MaxConnec *int `json:"maxConnec,omitempty"`

	// maxConnecPerTorrent is the maximum connections per torrent. -1 means unlimited.
	// +optional
	MaxConnecPerTorrent *int `json:"maxConnecPerTorrent,omitempty"`

	// maxUploads is the global maximum number of upload slots. -1 means unlimited.
	// +optional
	MaxUploads *int `json:"maxUploads,omitempty"`

	// maxUploadsPerTorrent is the maximum upload slots per torrent. -1 means unlimited.
	// +optional
	MaxUploadsPerTorrent *int `json:"maxUploadsPerTorrent,omitempty"`

	// maxRatioEnabled enables the global share ratio limit.
	// +optional
	MaxRatioEnabled *bool `json:"maxRatioEnabled,omitempty"`

	// maxRatio is the global share ratio limit (e.g., "2.0").
	// Represented as a string to preserve decimal precision in JSON.
	// +optional
	MaxRatio *string `json:"maxRatio,omitempty"`

	// maxRatioAction is what qBittorrent does when a share limit is reached.
	// +optional
	// +kubebuilder:validation:Enum=stop;remove;enableSuperSeeding;removeWithContent
	MaxRatioAction *string `json:"maxRatioAction,omitempty"`

	// maxSeedingTimeEnabled enables the global seeding time limit.
	// +optional
	MaxSeedingTimeEnabled *bool `json:"maxSeedingTimeEnabled,omitempty"`

	// maxSeedingTime is the global seeding time limit in minutes.
	// +optional
	MaxSeedingTime *int `json:"maxSeedingTime,omitempty"`

	// preallocateAll enables disk space pre-allocation for all files.
	// +optional
	PreallocateAll *bool `json:"preallocateAll,omitempty"`

	// locale is the UI locale (e.g., "en").
	// +optional
	Locale string `json:"locale,omitempty"`

	// webUiPort is the Web UI listening port.
	// +optional
	WebUIPort *int `json:"webUiPort,omitempty"`

	// bypassAuthSubnetWhitelistEnabled lets clients in the whitelisted subnets skip
	// the WebUI login, so an authenticating proxy does not produce a second prompt.
	// +optional
	BypassAuthSubnetWhitelistEnabled *bool `json:"bypassAuthSubnetWhitelistEnabled,omitempty"`

	// bypassAuthSubnetWhitelist is the comma-separated list of subnets allowed to
	// skip the WebUI login.
	// +optional
	BypassAuthSubnetWhitelist string `json:"bypassAuthSubnetWhitelist,omitempty"`

	// tempPath is where incomplete downloads go while tempPathEnabled is on.
	// +optional
	TempPath string `json:"tempPath,omitempty"`

	// incompleteFilesExt adds .!qB to the names of files still downloading.
	// +optional
	IncompleteFilesExt *bool `json:"incompleteFilesExt,omitempty"`

	// autoTmmEnabled puts new torrents in automatic management, which saves
	// them under their category's path.
	// +optional
	AutoTMMEnabled *bool `json:"autoTmmEnabled,omitempty"`

	// torrentChangedTmmEnabled moves a torrent in automatic management when its
	// category changes. When off, the torrent switches to manual management.
	// +optional
	TorrentChangedTMMEnabled *bool `json:"torrentChangedTmmEnabled,omitempty"`

	// savePathChangedTmmEnabled moves torrents in automatic management when
	// savePath changes. When off, they switch to manual management.
	// +optional
	SavePathChangedTMMEnabled *bool `json:"savePathChangedTmmEnabled,omitempty"`

	// categoryChangedTmmEnabled moves a category's torrents in automatic
	// management when its save path changes. When off, they switch to manual
	// management.
	// +optional
	CategoryChangedTMMEnabled *bool `json:"categoryChangedTmmEnabled,omitempty"`

	// torrentContentLayout decides whether a torrent's files go in a folder of
	// their own.
	// +optional
	// +kubebuilder:validation:Enum=Original;Subfolder;NoSubfolder
	TorrentContentLayout string `json:"torrentContentLayout,omitempty"`

	// addStoppedEnabled adds new torrents stopped.
	// +optional
	AddStoppedEnabled *bool `json:"addStoppedEnabled,omitempty"`

	// torrentStopCondition stops a new torrent once it gets this far.
	// +optional
	// +kubebuilder:validation:Enum=None;MetadataReceived;FilesChecked
	TorrentStopCondition string `json:"torrentStopCondition,omitempty"`

	// excludedFileNamesEnabled skips files whose names match excludedFileNames.
	// +optional
	ExcludedFileNamesEnabled *bool `json:"excludedFileNamesEnabled,omitempty"`

	// excludedFileNames are file name patterns to skip, such as *.lnk.
	// +optional
	ExcludedFileNames []string `json:"excludedFileNames,omitempty"`

	// dlLimit is the global download speed limit in KiB/s. 0 means unlimited.
	// +optional
	// +kubebuilder:validation:Minimum=0
	DlLimit *int `json:"dlLimit,omitempty"`

	// upLimit is the global upload speed limit in KiB/s. 0 means unlimited.
	// +optional
	// +kubebuilder:validation:Minimum=0
	UpLimit *int `json:"upLimit,omitempty"`

	// altDlLimit is the download speed limit in KiB/s while the alternative
	// limits are on. 0 means unlimited.
	// +optional
	// +kubebuilder:validation:Minimum=0
	AltDlLimit *int `json:"altDlLimit,omitempty"`

	// altUpLimit is the upload speed limit in KiB/s while the alternative limits
	// are on. 0 means unlimited.
	// +optional
	// +kubebuilder:validation:Minimum=0
	AltUpLimit *int `json:"altUpLimit,omitempty"`

	// schedulerEnabled turns the alternative limits on and off on a schedule.
	// +optional
	SchedulerEnabled *bool `json:"schedulerEnabled,omitempty"`

	// scheduleFromHour is the hour the alternative limits start.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=23
	ScheduleFromHour *int `json:"scheduleFromHour,omitempty"`

	// scheduleFromMin is the minute the alternative limits start.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=59
	ScheduleFromMin *int `json:"scheduleFromMin,omitempty"`

	// scheduleToHour is the hour the alternative limits end.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=23
	ScheduleToHour *int `json:"scheduleToHour,omitempty"`

	// scheduleToMin is the minute the alternative limits end.
	// +optional
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=59
	ScheduleToMin *int `json:"scheduleToMin,omitempty"`

	// schedulerDays is the days the schedule runs.
	// +optional
	// +kubebuilder:validation:Enum=everyDay;weekdays;weekends;monday;tuesday;wednesday;thursday;friday;saturday;sunday
	SchedulerDays string `json:"schedulerDays,omitempty"`

	// limitUtpRate applies the speed limits to µTP connections.
	// +optional
	LimitUTPRate *bool `json:"limitUtpRate,omitempty"`

	// limitTcpOverhead counts transport overhead against the speed limits.
	// +optional
	LimitTCPOverhead *bool `json:"limitTcpOverhead,omitempty"`

	// limitLanPeers applies the speed limits to peers on the local network.
	// +optional
	LimitLANPeers *bool `json:"limitLanPeers,omitempty"`

	// queueingEnabled limits how many torrents are active at once.
	// +optional
	QueueingEnabled *bool `json:"queueingEnabled,omitempty"`

	// maxActiveDownloads is how many torrents can download at once. -1 means unlimited.
	// +optional
	MaxActiveDownloads *int `json:"maxActiveDownloads,omitempty"`

	// maxActiveUploads is how many torrents can seed at once. -1 means unlimited.
	// +optional
	MaxActiveUploads *int `json:"maxActiveUploads,omitempty"`

	// maxActiveTorrents is how many torrents can be active at once. -1 means unlimited.
	// +optional
	MaxActiveTorrents *int `json:"maxActiveTorrents,omitempty"`

	// dontCountSlowTorrents leaves slow torrents out of the active limits.
	// +optional
	DontCountSlowTorrents *bool `json:"dontCountSlowTorrents,omitempty"`

	// slowTorrentDlRateThreshold is the download speed in KiB/s below which a
	// torrent counts as slow.
	// +optional
	// +kubebuilder:validation:Minimum=0
	SlowTorrentDlRateThreshold *int `json:"slowTorrentDlRateThreshold,omitempty"`

	// slowTorrentUlRateThreshold is the upload speed in KiB/s below which a
	// torrent counts as slow.
	// +optional
	// +kubebuilder:validation:Minimum=0
	SlowTorrentUlRateThreshold *int `json:"slowTorrentUlRateThreshold,omitempty"`

	// slowTorrentInactiveTimer is how many seconds a torrent stays under the
	// thresholds before it counts as slow.
	// +optional
	// +kubebuilder:validation:Minimum=0
	SlowTorrentInactiveTimer *int `json:"slowTorrentInactiveTimer,omitempty"`

	// listenPort is the port for incoming connections.
	// +optional
	// +kubebuilder:validation:Minimum=1
	// +kubebuilder:validation:Maximum=65535
	ListenPort *int `json:"listenPort,omitempty"`

	// randomPort picks a different listening port on each start.
	// +optional
	RandomPort *bool `json:"randomPort,omitempty"`

	// upnp forwards the listening port on the router with UPnP or NAT-PMP.
	// +optional
	UPnP *bool `json:"upnp,omitempty"`

	// networkInterface binds connections to one network interface, such as a
	// VPN's tun0. Empty means any interface.
	// +optional
	NetworkInterface *string `json:"networkInterface,omitempty"`

	// networkInterfaceAddress binds connections to one address of
	// networkInterface. Empty means all of them.
	// +optional
	NetworkInterfaceAddress *string `json:"networkInterfaceAddress,omitempty"`

	// anonymousMode hides the client's identity from peers and trackers.
	// +optional
	AnonymousMode *bool `json:"anonymousMode,omitempty"`

	// maxInactiveSeedingTimeEnabled enables the global limit on seeding time
	// without uploads.
	// +optional
	MaxInactiveSeedingTimeEnabled *bool `json:"maxInactiveSeedingTimeEnabled,omitempty"`

	// maxInactiveSeedingTime is the global inactive seeding time limit in minutes.
	// +optional
	MaxInactiveSeedingTime *int `json:"maxInactiveSeedingTime,omitempty"`
}

// QBittorrentCategory defines a download category with its save path.
type QBittorrentCategory struct {
	// name is the category name.
	// +required
	Name string `json:"name"`

	// savePath is the save path for this category.
	// +optional
	SavePath string `json:"savePath,omitempty"`
}

// QBittorrentConfigStatus defines the observed state of QBittorrentConfig.
type QBittorrentConfigStatus struct {
	// conditions represent the current state of the QBittorrentConfig resource.
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

// QBittorrentConfig is the Schema for the qbittorrentconfigs API.
type QBittorrentConfig struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec QBittorrentConfigSpec `json:"spec"`

	// +optional
	Status QBittorrentConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// QBittorrentConfigList contains a list of QBittorrentConfig.
type QBittorrentConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []QBittorrentConfig `json:"items"`
}

func (c *QBittorrentConfig) GetConditions() *[]metav1.Condition { return &c.Status.Conditions }
func (c *QBittorrentConfig) GetObservedGeneration() *int64      { return &c.Status.ObservedGeneration }
func (c *QBittorrentConfig) GetLastSyncTime() **metav1.Time     { return &c.Status.LastSyncTime }
func (c *QBittorrentConfig) GetReconcileConfig() *commonv1alpha1.ReconcileConfig {
	return c.Spec.Reconcile
}
func (c *QBittorrentConfig) GetManagedResources() *map[string][]string {
	return &c.Status.ManagedResources
}
