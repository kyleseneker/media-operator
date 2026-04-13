package bazarr

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	servarrv1alpha1 "github.com/kyleseneker/media-operator/api/servarr/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/engine"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	hc, err := engine.NewHTTPClient(srv.URL, engine.AuthFormEncoded,
		engine.WithAPIKey("test-key"),
		engine.WithAPIKeyHeader("X-API-KEY"),
		engine.WithTransport(&http.Transport{}),
	)
	require.NoError(t, err)
	return srv, NewClient(hc)
}

func TestPing(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/system/health", r.URL.Path)
		assert.Equal(t, "test-key", r.Header.Get("X-API-KEY"))
		w.WriteHeader(http.StatusOK)
	})
	assert.NoError(t, c.Ping(context.Background()))
}

func TestPostSettings(t *testing.T) {
	type generalSettings struct {
		IPAddr string `json:"ip"`
		Port   int    `json:"port"`
	}

	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/system/settings", r.URL.Path)
		assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "settings-general-ip=0.0.0.0")
		assert.Contains(t, string(body), "settings-general-port=6767")
		w.WriteHeader(http.StatusOK)
	})

	err := c.PostSettings(context.Background(), "general", generalSettings{IPAddr: "0.0.0.0", Port: 6767})
	assert.NoError(t, err)
}

func TestPostForm(t *testing.T) {
	_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/system/settings", r.URL.Path)
		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "custom=value")
		w.WriteHeader(http.StatusOK)
	})

	form := map[string][]string{"custom": {"value"}}
	err := c.PostForm(context.Background(), "/api/system/settings", form)
	assert.NoError(t, err)
}

func TestReconcileLanguages(t *testing.T) {
	tests := []struct {
		name       string
		langs      *servarrv1alpha1.BazarrLanguages
		expectCall bool
		checkBody  func(t *testing.T, body string)
	}{
		{
			name: "enabled languages only",
			langs: &servarrv1alpha1.BazarrLanguages{
				Enabled: []string{"en", "es"},
			},
			expectCall: true,
			checkBody: func(t *testing.T, body string) {
				assert.Contains(t, body, "settings-general-enabled_languages")
				assert.Contains(t, body, "en")
				assert.Contains(t, body, "es")
			},
		},
		{
			name:       "empty languages — no API call",
			langs:      &servarrv1alpha1.BazarrLanguages{},
			expectCall: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			_, c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				called = true
				if tt.checkBody != nil {
					body, _ := io.ReadAll(r.Body)
					tt.checkBody(t, string(body))
				}
				w.WriteHeader(http.StatusOK)
			})

			err := c.ReconcileLanguages(context.Background(), tt.langs)
			assert.NoError(t, err)
			assert.Equal(t, tt.expectCall, called)
		})
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
		obj     interface{}
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
