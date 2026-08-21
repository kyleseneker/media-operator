package transcode

import (
	"reflect"
	"testing"

	transcodev1alpha1 "github.com/kyleseneker/media-operator/api/transcode/v1alpha1"
)

// specFieldToDocKeys maps each TdarrLibrary field to the key(s) it must produce in
// Tdarr's library document. Tdarr accepts unknown keys and ignores missing ones
// while still answering 200, so an unmapped field is silently inert.
var specFieldToDocKeys = map[string][]string{
	"ID":                   {"_id"},
	"Name":                 {"name"},
	"Folder":               {"folder"},
	"Cache":                {"cache"},
	"Container":            {"container"},
	"ContainerFilter":      {"containerFilter"},
	"FoldersToIgnore":      {"foldersToIgnore"},
	"FolderWatching":       {"folderWatching"},
	"ProcessLibrary":       {"processLibrary"},
	"ProcessTranscodes":    {"processTranscodes"},
	"ProcessHealthChecks":  {"processHealthChecks"},
	"HealthCheckMode":      {"handbrakescan", "ffmpegscan"},
	"DecisionMode":         {"decisionMaker"},
	"ScanOnStart":          {"scanOnStart"},
	"ScheduledScanFindNew": {"scheduledScanFindNew"},
	"ScannerThreadCount":   {"scannerThreadCount"},
	"FFmpeg":               {"ffmpeg"},
	"Priority":             {"priority"},
	"FlowID":               {"flowId"},
	"PluginStack":          {"pluginStack"},
	"Variables":            {"variables"},
}

func fullyPopulatedLibrary() transcodev1alpha1.TdarrLibrary {
	return transcodev1alpha1.TdarrLibrary{
		ID: "movies", Name: "Movies", Folder: "/data/media/movies",
		Cache: "/temp", Container: "mkv", ContainerFilter: "mkv,mp4",
		FoldersToIgnore: "extras", FolderWatching: ptr(false),
		ProcessLibrary: ptr(true), ProcessTranscodes: ptr(true), ProcessHealthChecks: ptr(true),
		HealthCheckMode: "thorough", DecisionMode: "flows",
		ScanOnStart: ptr(true), ScheduledScanFindNew: ptr(true), ScannerThreadCount: ptr(1),
		FFmpeg: ptr(true), Priority: ptr(1), FlowID: "hevc-qsv",
		PluginStack: []string{"Tdarr_Plugin_x"},
		Variables:   map[string]string{"k": "v"},
	}
}

func ptr[T any](v T) *T { return &v }

func TestEveryLibraryFieldReachesTdarr(t *testing.T) {
	lib := fullyPopulatedLibrary()
	doc := buildTdarrLibraryDoc(lib)

	declared := reflect.TypeOf(lib)
	for i := range declared.NumField() {
		name := declared.Field(i).Name
		keys, mapped := specFieldToDocKeys[name]
		if !mapped {
			t.Errorf("spec field %s has no documented Tdarr key; Tdarr will accept the payload and ignore it", name)
			continue
		}
		for _, k := range keys {
			if _, ok := doc[k]; !ok {
				t.Errorf("spec field %s should emit key %q, but the document has no such key", name, k)
			}
		}
	}
}

// A field left unset must not emit its key, so an unset field never overwrites a
// value the CR does not manage.
func TestUnsetLibraryFieldsAreOmitted(t *testing.T) {
	doc := buildTdarrLibraryDoc(transcodev1alpha1.TdarrLibrary{ID: "movies", Name: "Movies"})

	alwaysEmitted := map[string]bool{"_id": true, "name": true, "schedule": true, "foldersToIgnore": true}
	for field, keys := range specFieldToDocKeys {
		for _, k := range keys {
			if alwaysEmitted[k] {
				continue
			}
			if _, present := doc[k]; present {
				t.Errorf("field %s emitted key %q despite being unset", field, k)
			}
		}
	}
}
