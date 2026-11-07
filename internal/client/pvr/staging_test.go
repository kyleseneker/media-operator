package pvr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	commonv1alpha1 "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/engine"
)

// A fresh app starts with no custom formats. The quality profile must still
// resolve its formatItems, which is only possible if the CF IDs are looked up
// after the custom formats have been written.
func TestQualityProfileResolvesCustomFormatsCreatedInTheSameRun(t *testing.T) {
	customFormats := []map[string]any{}
	var qpBodies []string
	nextID := 100

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "customformat"):
			if r.Method == http.MethodPost {
				var body map[string]any
				_ = json.NewDecoder(r.Body).Decode(&body)
				nextID++
				body["id"] = float64(nextID)
				customFormats = append(customFormats, body)
				w.WriteHeader(http.StatusCreated)
				_ = json.NewEncoder(w).Encode(body)
				return
			}
			_ = json.NewEncoder(w).Encode(customFormats)
		case strings.Contains(r.URL.Path, "qualityprofile"):
			if r.Method == http.MethodPost || r.Method == http.MethodPut {
				b := make([]byte, r.ContentLength)
				_, _ = r.Body.Read(b)
				qpBodies = append(qpBodies, string(b))
				w.WriteHeader(http.StatusCreated)
				_, _ = w.Write([]byte(`{"id":1}`))
				return
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		default:
			_ = json.NewEncoder(w).Encode([]map[string]any{})
		}
	}))
	defer srv.Close()

	hc, err := engine.NewHTTPClient(srv.URL, engine.AuthAPIKey, engine.WithAPIKey("k"), engine.WithAppLabel("sonarr"), engine.WithTransport(&http.Transport{}))
	require.NoError(t, err)

	opts := Options{
		CustomFormats: []commonv1alpha1.CustomFormat{{Name: "BR-DISK"}},
		QualityProfiles: []commonv1alpha1.QualityProfile{{
			Name:        "WEB-1080p",
			FormatItems: []commonv1alpha1.QualityProfileFormatItem{{Name: "BR-DISK", Score: 100}},
		}},
	}

	res, err := Reconcile(context.Background(), hc, "v3", struct{}{}, opts, engine.SyncPolicy{}, nil)
	require.NoError(t, err)
	require.Empty(t, res.Errors, "unexpected errors")
	require.Len(t, customFormats, 1, "expected the custom format to be created")
	require.NotEmpty(t, qpBodies, "quality profile was never written")
	assert.Contains(t, qpBodies[0], "101", "quality profile did not reference the newly created custom format id")
}
