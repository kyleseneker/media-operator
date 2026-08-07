package downloads

import (
	"reflect"
	"testing"

	downloadsv1alpha1 "github.com/kyleseneker/media-operator/api/downloads/v1alpha1"
)

func ptr[T any](v T) *T { return &v }

// qBittorrent silently ignores preference keys it does not recognise and still
// returns 200, so a wrong key name produces a green sync that changes nothing.
// These are the names its API actually accepts.
func TestQBPreferencePayloadUsesAPIKeyNames(t *testing.T) {
	prefs := &downloadsv1alpha1.QBittorrentPreferences{
		SavePath:              "/data/torrents",
		Locale:                "en",
		TempPathEnabled:       ptr(true),
		DHT:                   ptr(false),
		PEX:                   ptr(false),
		LSD:                   ptr(false),
		Encryption:            ptr(1),
		MaxConnec:             ptr(200),
		MaxConnecPerTorrent:   ptr(50),
		MaxUploads:            ptr(20),
		MaxUploadsPerTorrent:  ptr(5),
		MaxRatioEnabled:       ptr(true),
		MaxRatio:              ptr("2.0"),
		MaxRatioAction:        ptr("stop"),
		MaxSeedingTimeEnabled: ptr(true),
		MaxSeedingTime:        ptr(10080),
		PreallocateAll:        ptr(true),
		WebUIPort:             ptr(8080),
	}

	got, err := qbPreferencePayload(prefs)
	if err != nil {
		t.Fatalf("qbPreferencePayload: %v", err)
	}

	want := map[string]any{
		"save_path":                "/data/torrents",
		"locale":                   "en",
		"temp_path_enabled":        true,
		"dht":                      false,
		"pex":                      false,
		"lsd":                      false,
		"encryption":               1,
		"max_connec":               200,
		"max_connec_per_torrent":   50,
		"max_uploads":              20,
		"max_uploads_per_torrent":  5,
		"max_ratio_enabled":        true,
		"max_ratio":                2.0,
		"max_ratio_act":            0,
		"max_seeding_time_enabled": true,
		"max_seeding_time":         10080,
		"preallocate_all":          true,
		"web_ui_port":              8080,
	}

	if !reflect.DeepEqual(got, want) {
		t.Errorf("payload mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
}

// A field added to the spec but not mapped in qbPreferencePayload would be
// accepted by the CRD and then quietly dropped, so fail here instead.
func TestQBPreferencePayloadCoversEverySpecField(t *testing.T) {
	prefs := &downloadsv1alpha1.QBittorrentPreferences{}
	v := reflect.ValueOf(prefs).Elem()
	for i := range v.NumField() {
		f := v.Field(i)
		switch f.Kind() {
		case reflect.Pointer:
			f.Set(reflect.New(f.Type().Elem()))
		case reflect.String:
			f.SetString("x")
		}
	}
	// MaxRatio and MaxRatioAction parse their values, so give them valid ones.
	prefs.MaxRatio = ptr("1.0")
	prefs.MaxRatioAction = ptr("stop")

	got, err := qbPreferencePayload(prefs)
	if err != nil {
		t.Fatalf("qbPreferencePayload: %v", err)
	}
	if len(got) != v.NumField() {
		t.Errorf("spec has %d fields but payload emitted %d keys; an unmapped field would be silently dropped by qBittorrent", v.NumField(), len(got))
	}
}

func TestQBPreferencePayloadRejectsBadValues(t *testing.T) {
	if _, err := qbPreferencePayload(&downloadsv1alpha1.QBittorrentPreferences{MaxRatio: ptr("abc")}); err == nil {
		t.Error("expected error for unparseable maxRatio")
	}
	if _, err := qbPreferencePayload(&downloadsv1alpha1.QBittorrentPreferences{MaxRatioAction: ptr("nope")}); err == nil {
		t.Error("expected error for unknown maxRatioAction")
	}
}

func TestQBPreferencePayloadOmitsUnsetFields(t *testing.T) {
	got, err := qbPreferencePayload(&downloadsv1alpha1.QBittorrentPreferences{MaxRatioAction: ptr("removeWithContent")})
	if err != nil {
		t.Fatalf("qbPreferencePayload: %v", err)
	}
	if len(got) != 1 || got["max_ratio_act"] != 3 {
		t.Errorf("expected only max_ratio_act=3, got %#v", got)
	}
}
