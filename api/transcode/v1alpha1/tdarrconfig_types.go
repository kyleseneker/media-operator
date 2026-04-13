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

	// folderWatching enables watching the folder for new files.
	// +optional
	FolderWatching *bool `json:"folderWatching,omitempty"`

	// processLibrary enables processing of files in this library.
	// +optional
	ProcessLibrary *bool `json:"processLibrary,omitempty"`

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

	// flowPlugins is the raw JSON definition of flow plugin nodes.
	// +required
	// +kubebuilder:pruning:PreserveUnknownFields
	FlowPlugins runtime.RawExtension `json:"flowPlugins"`

	// flowEdges is the raw JSON definition of flow edge connections.
	// +required
	// +kubebuilder:pruning:PreserveUnknownFields
	FlowEdges runtime.RawExtension `json:"flowEdges"`
}

// TdarrWorkers configures the number of worker threads.
type TdarrWorkers struct {
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

func init() {
	SchemeBuilder.Register(&TdarrConfig{}, &TdarrConfigList{})
}
