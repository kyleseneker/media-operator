package pvr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
	if err != nil {
		t.Fatal(err)
	}

	opts := Options{
		CustomFormats: []commonv1alpha1.CustomFormat{{Name: "BR-DISK"}},
		QualityProfiles: []commonv1alpha1.QualityProfile{{
			Name:        "WEB-1080p",
			FormatItems: []commonv1alpha1.QualityProfileFormatItem{{Name: "BR-DISK", Score: 100}},
		}},
	}

	res, err := Reconcile(context.Background(), hc, "v3", struct{}{}, opts, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Errors) > 0 {
		t.Fatalf("unexpected errors: %v", res.Errors)
	}
	if len(customFormats) != 1 {
		t.Fatalf("expected the custom format to be created, got %d", len(customFormats))
	}
	if len(qpBodies) == 0 {
		t.Fatal("quality profile was never written")
	}
	if !strings.Contains(qpBodies[0], "101") {
		t.Errorf("quality profile did not reference the newly created custom format id;\nbody: %s", qpBodies[0])
	}
}
