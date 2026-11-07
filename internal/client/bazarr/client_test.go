package bazarr

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	subtitlesv1alpha1 "github.com/kyleseneker/media-operator/api/subtitles/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/engine"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	hc, err := engine.NewHTTPClient(srv.URL, engine.AuthFormEncoded,
		engine.WithAPIKey("test-key"),
		engine.WithAPIKeyHeader("X-API-KEY"),
		engine.WithTransport(&http.Transport{}),
	)
	require.NoError(t, err)
	return NewClient(hc)
}

func TestPing(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/system/health", r.URL.Path)
		assert.Equal(t, "test-key", r.Header.Get("X-API-KEY"))
		w.WriteHeader(http.StatusOK)
	})
	assert.NoError(t, c.Ping(context.Background()))
}

func TestPostForm(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/system/settings", r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "custom=value")
		w.WriteHeader(http.StatusOK)
	})

	form := map[string][]string{"custom": {"value"}}
	err := c.PostForm(context.Background(), "/api/system/settings", form)
	assert.NoError(t, err)
}

func TestLanguagesForm(t *testing.T) {
	form, err := LanguagesForm(&subtitlesv1alpha1.BazarrLanguages{
		Enabled:  []string{"en", "es"},
		Profiles: []subtitlesv1alpha1.BazarrLanguageProfile{{Name: "English", Items: []subtitlesv1alpha1.BazarrLanguageItem{{Language: "en"}}}},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"en", "es"}, form[EnabledLanguagesField], "Bazarr reads one value per enabled language")
	assert.Contains(t, form.Get("languages-profiles"), `"name":"English"`)

	empty, err := LanguagesForm(&subtitlesv1alpha1.BazarrLanguages{})
	require.NoError(t, err)
	assert.Empty(t, empty, "nothing declared means nothing to send")
}

// A Secret reference is not a setting; posting it stored its printed form in
// Bazarr's config as sonarr.api_key_secret_ref.
func TestStructToFormDataSkipsNonScalarFields(t *testing.T) {
	form := StructToFormData("sonarr", &subtitlesv1alpha1.BazarrAppConnection{
		Host:            "sonarr",
		APIKeySecretRef: commonv1alpha1.SecretKeyRef{Name: "creds", Key: "api-key"},
	})
	assert.Equal(t, "sonarr", form.Get("settings-sonarr-ip"))
	for key := range form {
		assert.NotContains(t, key, "secret", "a Secret reference leaked into the settings form")
	}
}

func TestStructToFormData(t *testing.T) {
	type subSyncSettings struct {
		UseSubSync  *bool  `json:"use_subsync"`
		Threshold   *int   `json:"subsync_threshold"`
		Description string `json:"description"`
		Skip        string `json:"skip,omitempty"`
		Ignored     string `json:"-"`
	}

	truth := true
	threshold := 90

	tests := []struct {
		name    string
		section string
		obj     any
		want    map[string]string
		absent  []string
	}{
		{
			name:    "all fields set",
			section: "subsync",
			obj: subSyncSettings{
				UseSubSync:  &truth,
				Threshold:   &threshold,
				Description: "Audio sync",
			},
			want: map[string]string{
				"settings-subsync-use_subsync":       "true",
				"settings-subsync-subsync_threshold": "90",
				"settings-subsync-description":       "Audio sync",
			},
		},
		{
			name:    "nil pointers and empty strings skipped",
			section: "subsync",
			obj: subSyncSettings{
				UseSubSync:  nil,
				Threshold:   nil,
				Description: "",
			},
			absent: []string{
				"settings-subsync-use_subsync",
				"settings-subsync-subsync_threshold",
				"settings-subsync-description",
			},
		},
		{
			name:    "json:-  field ignored",
			section: "general",
			obj: subSyncSettings{
				Ignored:     "should-not-appear",
				Description: "visible",
			},
			want: map[string]string{
				"settings-general-description": "visible",
			},
			absent: []string{
				"settings-general-Ignored",
				"settings-general--",
			},
		},
		{
			name:    "pointer to struct",
			section: "general",
			obj:     &subSyncSettings{UseSubSync: &truth},
			want: map[string]string{
				"settings-general-use_subsync": "true",
			},
		},
		{
			name:    "nil pointer returns empty form",
			section: "general",
			obj:     (*subSyncSettings)(nil),
			absent:  []string{"settings-general-use_subsync"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			form := StructToFormData(tt.section, tt.obj)
			for k, v := range tt.want {
				assert.Equal(t, v, form.Get(k), "key %q", k)
			}
			for _, k := range tt.absent {
				assert.Empty(t, form.Get(k), "key %q should be absent", k)
			}
		})
	}
}

func TestStructToFormDataUsesBazarrSettingNames(t *testing.T) {
	conn := &subtitlesv1alpha1.BazarrAppConnection{
		Host:     "arr-sonarr.arr.svc.cluster.local",
		BasePath: "/sonarr",
	}
	form := StructToFormData("sonarr", conn)

	assert.Equal(t, "arr-sonarr.arr.svc.cluster.local", form.Get("settings-sonarr-ip"))
	assert.Equal(t, "/sonarr", form.Get("settings-sonarr-base_url"))
	assert.Empty(t, form.Get("settings-sonarr-host"), "host is inert in Bazarr; the real key is ip")
	assert.Empty(t, form.Get("settings-sonarr-basePath"))
}

func TestSettingNameConvertsCamelCaseAndHonoursOverrides(t *testing.T) {
	cases := []struct{ section, in, want string }{
		{"general", "useSonarr", "use_sonarr"},
		{"general", "minimumScoreMovie", "minimum_score_movie"},
		{"general", "multithreading", "multithreading"},
		{"sonarr", "onlyMonitored", "only_monitored"},
		{"sonarr", "httpTimeout", "http_timeout"},
		{"sonarr", "host", "ip"},
		{"sonarr", "syncInterval", "series_sync"},
		{"radarr", "syncInterval", "movies_sync"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, SettingName(c.section, c.in), "SettingName(%q, %q)", c.section, c.in)
	}
}

func TestBuildLanguageProfilesMatchesBazarrSchema(t *testing.T) {
	hi := true
	profiles := []subtitlesv1alpha1.BazarrLanguageProfile{
		{Name: "English", Items: []subtitlesv1alpha1.BazarrLanguageItem{{Language: "en", HI: &hi}}},
	}
	out := buildLanguageProfiles(profiles)
	require.Len(t, out, 1)
	p := out[0]

	for _, k := range []string{"profileId", "name", "items", "cutoff", "mustContain", "mustNotContain", "originalFormat"} {
		assert.Contains(t, p, k, "Bazarr indexes %q unconditionally and raises KeyError", k)
	}
	assert.Equal(t, 1, p["profileId"], "profileId defaults to a 1-based index")

	items := p["items"].([]map[string]any)
	require.Len(t, items, 1)
	assert.Equal(t, "en", items[0]["language"])
	assert.Equal(t, "True", items[0]["hi"], "Bazarr stores item flags as True/False strings")
	assert.Equal(t, "False", items[0]["forced"])
	assert.Equal(t, "False", items[0]["audio_exclude"])
}
