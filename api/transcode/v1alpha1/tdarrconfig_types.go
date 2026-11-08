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
	"k8s.io/apimachinery/pkg/runtime"
)

// TdarrConfigSpec defines the desired state of TdarrConfig.
type TdarrConfigSpec struct {
	// connection defines how to connect to the Tdarr instance using an API key.
	// +required
	Connection commonv1alpha1.AppConnection `json:"connection"`

	// reconcile configures reconciliation behavior (e.g., sync interval).
	// +optional
	Reconcile *commonv1alpha1.ReconcileConfig `json:"reconcile,omitempty"`

	// libraries defines media libraries to manage in Tdarr.
	// +optional
	Libraries []TdarrLibrary `json:"libraries,omitempty"`

	// flows defines transcoding flows (plugins and edges).
	// +optional
	Flows []TdarrFlow `json:"flows,omitempty"`

	// workers configures the number of worker threads by type.
	// +optional
	Workers *TdarrWorkers `json:"workers,omitempty"`

	// settings configures Tdarr's server-wide options. Pausing nodes is left to
	// whoever runs Tdarr.
	// +optional
	Settings *TdarrSettings `json:"settings,omitempty"`
}

// TdarrSettings configures Tdarr's server-wide options.
type TdarrSettings struct {
	// autoAcceptTranscodes moves a successful transcode into the library or
	// output folder without waiting in staging for review.
	// +optional
	AutoAcceptTranscodes *bool `json:"autoAcceptTranscodes,omitempty"`

	// stagedFileLimit is how many files can wait in staging for review.
	// +optional
	// +kubebuilder:validation:Minimum=0
	StagedFileLimit *int `json:"stagedFileLimit,omitempty"`

	// autoPauseIfCacheFull pauses every node when a library cache has less
	// free space than autoPauseIfCacheFullThreshold.
	// +optional
	AutoPauseIfCacheFull *bool `json:"autoPauseIfCacheFull,omitempty"`

	// autoPauseIfCacheFullThreshold is the free cache space, in GB, below which
	// autoPauseIfCacheFull pauses the nodes.
	// +optional
	// +kubebuilder:validation:Minimum=0
	AutoPauseIfCacheFullThreshold *int `json:"autoPauseIfCacheFullThreshold,omitempty"`

	// workerStallDetector cancels workers that stall.
	// +optional
	WorkerStallDetector *bool `json:"workerStallDetector,omitempty"`

	// workerStallDetectorInterval is how often, in seconds, workers are checked
	// for stalls.
	// +optional
	// +kubebuilder:validation:Minimum=1
	WorkerStallDetectorInterval *int `json:"workerStallDetectorInterval,omitempty"`

	// ignoreSchedules runs workers outside their libraries' schedules.
	// +optional
	IgnoreSchedules *bool `json:"ignoreSchedules,omitempty"`

	// alternateLibraries takes queued files from each library in turn.
	// +optional
	AlternateLibraries *bool `json:"alternateLibraries,omitempty"`

	// prioritiseLibraries takes queued files by library priority.
	// +optional
	PrioritiseLibraries *bool `json:"prioritiseLibraries,omitempty"`

	// prioritiseTranscodes takes transcodes before health checks.
	// +optional
	PrioritiseTranscodes *bool `json:"prioritiseTranscodes,omitempty"`

	// prioritiseHealthChecks takes health checks before transcodes.
	// +optional
	PrioritiseHealthChecks *bool `json:"prioritiseHealthChecks,omitempty"`

	// runMkvpropedit runs mkvpropedit on files before the plugins.
	// +optional
	RunMkvpropedit *bool `json:"runMkvpropedit,omitempty"`

	// pluginAutoUpdate keeps the community plugins up to date.
	// +optional
	PluginAutoUpdate *bool `json:"pluginAutoUpdate,omitempty"`

	// jobHistorySizeLimitGB caps the job history, in GB.
	// +optional
	// +kubebuilder:validation:Minimum=0
	JobHistorySizeLimitGB *int `json:"jobHistorySizeLimitGB,omitempty"`

	// backupLimit is how many server backups are kept.
	// +optional
	// +kubebuilder:validation:Minimum=0
	BackupLimit *int `json:"backupLimit,omitempty"`

	// logFullCliOutput logs the whole FFmpeg and HandBrake output of each job.
	// +optional
	LogFullCliOutput *bool `json:"logFullCliOutput,omitempty"`

	// verboseLogs enables verbose logging.
	// +optional
	VerboseLogs *bool `json:"verboseLogs,omitempty"`

	// notificationsDiscordWebhookSecretRef references the Discord webhook URL
	// notifications go to.
	// +optional
	NotificationsDiscordWebhookSecretRef *commonv1alpha1.SecretKeyRef `json:"notificationsDiscordWebhookSecretRef,omitempty"`

	// notificationsTranscodeSuccess notifies when a transcode succeeds.
	// +optional
	NotificationsTranscodeSuccess *bool `json:"notificationsTranscodeSuccess,omitempty"`

	// notificationsTranscodeError notifies when a transcode fails.
	// +optional
	NotificationsTranscodeError *bool `json:"notificationsTranscodeError,omitempty"`

	// notificationsTranscodeCancelled notifies when a transcode is cancelled.
	// +optional
	NotificationsTranscodeCancelled *bool `json:"notificationsTranscodeCancelled,omitempty"`

	// notificationsHealthcheckSuccess notifies when a health check succeeds.
	// +optional
	NotificationsHealthcheckSuccess *bool `json:"notificationsHealthcheckSuccess,omitempty"`

	// notificationsHealthcheckError notifies when a health check fails.
	// +optional
	NotificationsHealthcheckError *bool `json:"notificationsHealthcheckError,omitempty"`

	// notificationsHealthcheckCancelled notifies when a health check is cancelled.
	// +optional
	NotificationsHealthcheckCancelled *bool `json:"notificationsHealthcheckCancelled,omitempty"`

	// notificationsRequireReview notifies when a file waits in staging for review.
	// +optional
	NotificationsRequireReview *bool `json:"notificationsRequireReview,omitempty"`

	// notificationsServerStarted notifies when the server starts.
	// +optional
	NotificationsServerStarted *bool `json:"notificationsServerStarted,omitempty"`

	// notificationsServerUpdateReady notifies when a server update is ready.
	// +optional
	NotificationsServerUpdateReady *bool `json:"notificationsServerUpdateReady,omitempty"`

	// notificationsCustomText is added to every notification.
	// +optional
	NotificationsCustomText string `json:"notificationsCustomText,omitempty"`
}

// TdarrLibrary defines a media library in Tdarr.
type TdarrLibrary struct {
	// id is the Tdarr document ID for this library.
	// +required
	ID string `json:"id"`

	// name is the display name of the library.
	// +required
	Name string `json:"name"`

	// folder is the source media folder path.
	// +required
	Folder string `json:"folder"`

	// cache is the transcode cache folder path.
	// +optional
	Cache string `json:"cache,omitempty"`

	// container is the output container format (e.g., "mkv", "mp4").
	// +optional
	Container string `json:"container,omitempty"`

	// containerFilter is the container filter for scanning (e.g., "mkv,mp4,avi").
	// +optional
	ContainerFilter string `json:"containerFilter,omitempty"`

	// foldersToIgnore is a comma-separated list of folder names to skip while scanning.
	// +optional
	FoldersToIgnore string `json:"foldersToIgnore,omitempty"`

	// foldersToIgnoreCaseInsensitive matches foldersToIgnore regardless of case.
	// +optional
	FoldersToIgnoreCaseInsensitive *bool `json:"foldersToIgnoreCaseInsensitive,omitempty"`

	// filterResolutionsSkip is a comma-separated list of resolutions, such as
	// 720p,1080p, whose files the server skips without sending them to a worker.
	// +optional
	FilterResolutionsSkip string `json:"filterResolutionsSkip,omitempty"`

	// filterCodecsSkip is a comma-separated list of video codecs, such as
	// hevc,vp9, whose files the server skips.
	// +optional
	FilterCodecsSkip string `json:"filterCodecsSkip,omitempty"`

	// filterContainersSkip is a comma-separated list of containers, such as
	// mkv,mp4, whose files the server skips.
	// +optional
	FilterContainersSkip string `json:"filterContainersSkip,omitempty"`

	// filterHardlinked skips files linked in more than one place until they no
	// longer are.
	// +optional
	FilterHardlinked *bool `json:"filterHardlinked,omitempty"`

	// useFsEvents watches the folder with file system events instead of polling.
	// +optional
	UseFsEvents *bool `json:"useFsEvents,omitempty"`

	// folderWatchScanInterval is how often, in seconds, folder watching polls.
	// +optional
	// +kubebuilder:validation:Minimum=1
	FolderWatchScanInterval *int `json:"folderWatchScanInterval,omitempty"`

	// folderWatching enables watching the folder for new files.
	// +optional
	FolderWatching *bool `json:"folderWatching,omitempty"`

	// processLibrary enables processing of files in this library.
	// +optional
	ProcessLibrary *bool `json:"processLibrary,omitempty"`

	// processTranscodes enables transcode jobs for this library.
	// +optional
	ProcessTranscodes *bool `json:"processTranscodes,omitempty"`

	// processHealthChecks enables health check jobs for this library.
	// +optional
	ProcessHealthChecks *bool `json:"processHealthChecks,omitempty"`

	// healthCheckMode selects the health check to run. quick uses HandBrake to
	// read file headers; thorough decodes every frame with FFmpeg.
	// +kubebuilder:validation:Enum=quick;thorough
	// +optional
	HealthCheckMode string `json:"healthCheckMode,omitempty"`

	// decisionMode selects how Tdarr decides what to do with a file.
	// +kubebuilder:validation:Enum=flows;plugins
	// +optional
	DecisionMode string `json:"decisionMode,omitempty"`

	// scanOnStart enables scanning the library when Tdarr starts.
	// +optional
	ScanOnStart *bool `json:"scanOnStart,omitempty"`

	// scheduledScanFindNew enables scheduled scans for new files.
	// +optional
	ScheduledScanFindNew *bool `json:"scheduledScanFindNew,omitempty"`

	// scannerThreadCount is the number of threads used for scanning.
	// +optional
	ScannerThreadCount *int `json:"scannerThreadCount,omitempty"`

	// ffmpeg enables using FFmpeg (true) vs HandBrake (false) for transcoding.
	// +optional
	FFmpeg *bool `json:"ffmpeg,omitempty"`

	// priority is the processing priority for this library. Lower values are higher priority.
	// +optional
	Priority *int `json:"priority,omitempty"`

	// flowId is the Tdarr flow this library processes files with. Flows are the
	// current mechanism; classic plugin stacks are the older alternative.
	// +optional
	FlowID string `json:"flowId,omitempty"`

	// pluginStack lists the community plugin IDs to run, in order, for libraries
	// not using a flow.
	// +optional
	PluginStack []string `json:"pluginStack,omitempty"`

	// variables are user-defined library variables. Flows read them as
	// {{{args.variables.user.<name>}}}, which is how community flows are
	// configured without editing the flow itself. A variable removed from this
	// map is deleted from Tdarr only with spec.reconcile.prune, and only if the
	// operator created it.
	// +optional
	Variables map[string]string `json:"variables,omitempty"`
}

// TdarrFlow defines a transcoding flow with plugins and edges.
type TdarrFlow struct {
	// id is the Tdarr document ID for this flow.
	// +required
	ID string `json:"id"`

	// name is the display name of the flow.
	// +required
	Name string `json:"name"`

	// description is a human-readable description of the flow.
	// +optional
	Description string `json:"description,omitempty"`

	// flowPlugins is the raw JSON definition of flow plugin nodes, as Tdarr
	// exports it. Quote the y key of each node's position ("y": 200): YAML 1.1
	// parsers, kubectl's included, read a bare y as the boolean true.
	// +required
	// +kubebuilder:pruning:PreserveUnknownFields
	FlowPlugins []runtime.RawExtension `json:"flowPlugins"`

	// flowEdges is the raw JSON definition of flow edge connections.
	// +required
	// +kubebuilder:pruning:PreserveUnknownFields
	FlowEdges []runtime.RawExtension `json:"flowEdges"`
}

// TdarrWorkers configures the number of worker threads on one Tdarr node.
type TdarrWorkers struct {
	// nodeName is the name of the Tdarr node these limits are for. It may be
	// left out while Tdarr has a single node, and is required once it has more.
	// +optional
	NodeName string `json:"nodeName,omitempty"`

	// transcodeGPU is the number of GPU transcode workers.
	// +optional
	TranscodeGPU *int `json:"transcodeGPU,omitempty"`

	// transcodeCPU is the number of CPU transcode workers.
	// +optional
	TranscodeCPU *int `json:"transcodeCPU,omitempty"`

	// healthcheckGPU is the number of GPU health check workers.
	// +optional
	HealthcheckGPU *int `json:"healthcheckGPU,omitempty"`

	// healthcheckCPU is the number of CPU health check workers.
	// +optional
	HealthcheckCPU *int `json:"healthcheckCPU,omitempty"`
}

// TdarrConfigStatus defines the observed state of TdarrConfig.
type TdarrConfigStatus struct {
	// conditions represent the current state of the TdarrConfig resource.
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

// TdarrConfig is the Schema for the tdarrconfigs API.
type TdarrConfig struct {
	metav1.TypeMeta `json:",inline"`

	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required
	Spec TdarrConfigSpec `json:"spec"`

	// +optional
	Status TdarrConfigStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TdarrConfigList contains a list of TdarrConfig.
type TdarrConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TdarrConfig `json:"items"`
}

func (c *TdarrConfig) GetConditions() *[]metav1.Condition                  { return &c.Status.Conditions }
func (c *TdarrConfig) GetObservedGeneration() *int64                       { return &c.Status.ObservedGeneration }
func (c *TdarrConfig) GetLastSyncTime() **metav1.Time                      { return &c.Status.LastSyncTime }
func (c *TdarrConfig) GetReconcileConfig() *commonv1alpha1.ReconcileConfig { return c.Spec.Reconcile }
func (c *TdarrConfig) GetManagedResources() *map[string][]string           { return &c.Status.ManagedResources }
