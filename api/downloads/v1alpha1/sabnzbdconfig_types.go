package v1alpha1

import (
	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SabnzbdConfigSpec defines the desired state of SabnzbdConfig.
type SabnzbdConfigSpec struct {
	// connection defines how to connect to the SABnzbd instance.
	// +required
	Connection commonv1alpha1.AppConnection `json:"connection"`

	// reconcile configures reconciliation behavior (e.g., sync interval).
	// +optional
	Reconcile *commonv1alpha1.ReconcileConfig `json:"reconcile,omitempty"`

	// general configures general SABnzbd settings.
	// +optional
	General *SabnzbdGeneral `json:"general,omitempty"`

	// servers defines Usenet server connections.
	// +optional
	Servers []SabnzbdServer `json:"servers,omitempty"`

	// categories defines download categories.
	// +optional
	Categories []SabnzbdCategory `json:"categories,omitempty"`

	// folders configures download and temp folder paths.
	// +optional
	Folders *SabnzbdFolders `json:"folders,omitempty"`
}

// SabnzbdGeneral configures general SABnzbd settings.
type SabnzbdGeneral struct {
	// downloadSpeedLimit is the download speed limit (e.g., "100M", "0" for unlimited).
	// +optional
	DownloadSpeedLimit string `json:"downloadSpeedLimit,omitempty"`

	// pauseOnPostProcessing pauses downloading during post-processing.
	// +optional
	PauseOnPostProcessing *bool `json:"pauseOnPostProcessing,omitempty"`

	// scriptDir is the path to the scripts directory.
	// Deprecated: use folders.scriptDir, which takes precedence when both are set.
	// +optional
	ScriptDir string `json:"scriptDir,omitempty"`

	// autoSort sorts the queue by age whenever an NZB is added.
	// +optional
	AutoSort *bool `json:"autoSort,omitempty"`

	// preCheck enables NZB pre-check for completion.
	// +optional
	PreCheck *bool `json:"preCheck,omitempty"`

	// topOnly fetches articles only for the job at the top of the queue.
	// +optional
	TopOnly *bool `json:"topOnly,omitempty"`

	// failHopelessJobs fails a job as soon as it can't be completed.
	// +optional
	FailHopelessJobs *bool `json:"failHopelessJobs,omitempty"`

	// encryptedRarAction is what SABnzbd does with a job containing an encrypted RAR.
	// +optional
	// +kubebuilder:validation:Enum=off;pause;fail
	EncryptedRarAction string `json:"encryptedRarAction,omitempty"`

	// unwantedExtensions lists file extensions, without the dot, that
	// unwantedExtensionsAction applies to.
	// +optional
	UnwantedExtensions []string `json:"unwantedExtensions,omitempty"`

	// unwantedExtensionsMode decides whether unwantedExtensions lists the
	// extensions to act on or the only ones allowed.
	// +optional
	// +kubebuilder:validation:Enum=blocklist;allowlist
	UnwantedExtensionsMode string `json:"unwantedExtensionsMode,omitempty"`

	// unwantedExtensionsAction is what SABnzbd does with a job containing an
	// unwanted extension.
	// +optional
	// +kubebuilder:validation:Enum=off;pause;fail
	UnwantedExtensionsAction string `json:"unwantedExtensionsAction,omitempty"`

	// directUnpack unpacks a job while it's still downloading.
	// +optional
	DirectUnpack *bool `json:"directUnpack,omitempty"`

	// enableAllPar downloads every par2 file, not only those a repair needs.
	// +optional
	EnableAllPar *bool `json:"enableAllPar,omitempty"`

	// sfvCheck verifies jobs without par2 files against their SFV files.
	// +optional
	SFVCheck *bool `json:"sfvCheck,omitempty"`

	// safePostProcessing post-processes only jobs that passed verification.
	// +optional
	SafePostProcessing *bool `json:"safePostProcessing,omitempty"`

	// enableRecursive unpacks archives found inside archives.
	// +optional
	EnableRecursive *bool `json:"enableRecursive,omitempty"`

	// flatUnpack ignores the folders inside archives when unpacking.
	// +optional
	FlatUnpack *bool `json:"flatUnpack,omitempty"`

	// ignoreSamples skips sample files.
	// +optional
	IgnoreSamples *bool `json:"ignoreSamples,omitempty"`

	// deobfuscateFinalFilenames renames obfuscated files after the job.
	// +optional
	DeobfuscateFinalFilenames *bool `json:"deobfuscateFinalFilenames,omitempty"`

	// scriptCanFail lets a job's post-processing script fail without failing the job.
	// +optional
	ScriptCanFail *bool `json:"scriptCanFail,omitempty"`

	// cleanupList lists file extensions, without the dot, to delete after a job.
	// +optional
	CleanupList []string `json:"cleanupList,omitempty"`

	// historyRetention decides how long jobs stay in the history.
	// +optional
	// +kubebuilder:validation:Enum=all;number-archive;number-delete;days-archive;days-delete;all-archive;all-delete
	HistoryRetention string `json:"historyRetention,omitempty"`

	// historyRetentionNumber is the number of jobs or days historyRetention keeps.
	// +optional
	// +kubebuilder:validation:Minimum=0
	HistoryRetentionNumber *int `json:"historyRetentionNumber,omitempty"`

	// permissions are the octal permissions set on completed downloads, such as 775.
	// +optional
	// +kubebuilder:validation:Pattern=`^[0-7]{3,4}$`
	Permissions string `json:"permissions,omitempty"`

	// downloadFree is the free space the incomplete folder needs to keep
	// downloading, such as 500M.
	// +optional
	DownloadFree string `json:"downloadFree,omitempty"`

	// completeFree is the free space the complete folder needs to keep
	// downloading, such as 10G.
	// +optional
	CompleteFree string `json:"completeFree,omitempty"`

	// cacheLimit is the memory used to cache articles, such as 1G.
	// +optional
	CacheLimit string `json:"cacheLimit,omitempty"`

	// propagationDelay holds back jobs posted less than this many minutes ago.
	// +optional
	// +kubebuilder:validation:Minimum=0
	PropagationDelay *int `json:"propagationDelay,omitempty"`

	// hostWhitelist lists the host names SABnzbd answers on besides IP
	// addresses, localhost and names ending in .local, which cover
	// *.svc.cluster.local. It replaces SABnzbd's list, so it must include the
	// host in connection.url unless that is one of those.
	// +optional
	HostWhitelist []string `json:"hostWhitelist,omitempty"`
}

// SabnzbdServer defines a Usenet server connection.
type SabnzbdServer struct {
	// name is the display name of the server.
	// +required
	Name string `json:"name"`

	// host is the hostname of the Usenet server.
	// +required
	Host string `json:"host"`

	// port is the port number.
	// +required
	Port int `json:"port"`

	// ssl enables SSL/TLS connection.
	// +optional
	SSL *bool `json:"ssl,omitempty"`

	// usernameSecretRef references a Secret containing the username.
	// +optional
	UsernameSecretRef *commonv1alpha1.SecretKeyRef `json:"usernameSecretRef,omitempty"`

	// passwordSecretRef references a Secret containing the password.
	// +optional
	PasswordSecretRef *commonv1alpha1.SecretKeyRef `json:"passwordSecretRef,omitempty"`

	// connections is the number of simultaneous connections.
	// +optional
	Connections *int `json:"connections,omitempty"`

	// priority is the server priority. 0 = highest.
	// +optional
	Priority *int `json:"priority,omitempty"`

	// retention is the server retention in days. 0 = unlimited.
	// +optional
	Retention *int `json:"retention,omitempty"`

	// enable controls whether this server is active.
	// +optional
	// +kubebuilder:default=true
	Enable *bool `json:"enable,omitempty"`

	// sslVerify is how strictly the server's certificate is checked when ssl is on.
	// +optional
	// +kubebuilder:validation:Enum=strict;medium;minimal;disabled
	SSLVerify string `json:"sslVerify,omitempty"`

	// optional lets SABnzbd stop using the server for a while when it fails.
	// +optional
	Optional *bool `json:"optional,omitempty"`

	// required makes SABnzbd wait for the server when it fails instead of
	// skipping it.
	// +optional
	Required *bool `json:"required,omitempty"`

	// timeout is how many seconds SABnzbd waits for the server to answer.
	// +optional
	// +kubebuilder:validation:Minimum=1
	Timeout *int `json:"timeout,omitempty"`
}

// SabnzbdCategory defines a download category.
type SabnzbdCategory struct {
	// name is the category name.
	// +required
	Name string `json:"name"`

	// dir is the subdirectory for this category (relative to complete dir).
	// +optional
	Dir string `json:"dir,omitempty"`

	// script is the post-processing script for this category.
	// +optional
	Script string `json:"script,omitempty"`

	// priority is the download priority. -100=default, -2=paused, -1=low, 0=normal, 1=high, 2=force.
	// +optional
	Priority *int `json:"priority,omitempty"`

	// postProcessing is how far SABnzbd processes the category's jobs: download
	// only, repair, unpack, or unpack and delete the archives.
	// +optional
	// +kubebuilder:validation:Enum=download;repair;unpack;delete
	PostProcessing string `json:"postProcessing,omitempty"`
}

// SabnzbdFolders configures folder paths.
type SabnzbdFolders struct {
	// completeDir is the path for completed downloads.
	// +optional
	CompleteDir string `json:"completeDir,omitempty"`

	// incompleteDir is the path for incomplete downloads.
	// +optional
	IncompleteDir string `json:"incompleteDir,omitempty"`

	// tempDownloadDir is the temporary download path.
	// +optional
	TempDownloadDir string `json:"tempDownloadDir,omitempty"`

	// nzbBackupDir is the path for NZB backups.
	// +optional
	NzbBackupDir string `json:"nzbBackupDir,omitempty"`

	// scriptDir is the path for scripts.
	// +optional
	ScriptDir string `json:"scriptDir,omitempty"`
}

// SabnzbdConfigStatus defines the observed state of SabnzbdConfig.
type SabnzbdConfigStatus struct {
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions         []metav1.Condition `json:"conditions,omitempty"`
	ObservedGeneration int64              `json:"observedGeneration,omitempty"`
	LastSyncTime       *metav1.Time       `json:"lastSyncTime,omitempty"`

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

// SabnzbdConfig is the Schema for the sabnzbdconfigs API.
type SabnzbdConfig struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec SabnzbdConfigSpec `json:"spec"`

	// +optional
	Status SabnzbdConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// SabnzbdConfigList contains a list of SabnzbdConfig.
type SabnzbdConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []SabnzbdConfig `json:"items"`
}

func (c *SabnzbdConfig) GetConditions() *[]metav1.Condition                  { return &c.Status.Conditions }
func (c *SabnzbdConfig) GetObservedGeneration() *int64                       { return &c.Status.ObservedGeneration }
func (c *SabnzbdConfig) GetLastSyncTime() **metav1.Time                      { return &c.Status.LastSyncTime }
func (c *SabnzbdConfig) GetReconcileConfig() *commonv1alpha1.ReconcileConfig { return c.Spec.Reconcile }
func (c *SabnzbdConfig) GetManagedResources() *map[string][]string           { return &c.Status.ManagedResources }
