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

// schemaServer serves only the schema endpoints, so a request for anything else
// stands out as unexpected.
func schemaServer(t *testing.T) *engine.HTTPClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/downloadclient/schema") {
			_ = json.NewEncoder(w).Encode(qbitSchema())
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	t.Cleanup(srv.Close)

	hc, err := engine.NewHTTPClient(srv.URL, engine.AuthAPIKey,
		engine.WithAPIKey("k"), engine.WithAPIKeyHeader("X-Api-Key"),
		engine.WithTransport(&http.Transport{}))
	if err != nil {
		t.Fatalf("building client: %v", err)
	}
	return hc
}

func TestValidateOptionFieldsReportsTypedFieldName(t *testing.T) {
	opts := Options{DownloadClients: []commonv1alpha1.DownloadClient{{
		Name: "qbit", Implementation: "QBittorrent",
		Fields: []commonv1alpha1.ConfigField{{Name: "tvCatagory"}},
	}}}

	problems := ValidateOptionFields(context.Background(), schemaServer(t), "v3", opts)
	if len(problems) != 1 {
		t.Fatalf("expected one problem, got %v", problems)
	}
	if !strings.Contains(problems[0], "qbit") || !strings.Contains(problems[0], "tvCatagory") {
		t.Errorf("problem does not identify the resource and field: %s", problems[0])
	}
}

func TestValidateOptionFieldsPassesKnownFields(t *testing.T) {
	opts := Options{DownloadClients: []commonv1alpha1.DownloadClient{{
		Name: "qbit", Implementation: "QBittorrent",
		Fields: []commonv1alpha1.ConfigField{{Name: "tvCategory"}},
	}}}

	if problems := ValidateOptionFields(context.Background(), schemaServer(t), "v3", opts); len(problems) != 0 {
		t.Errorf("valid field reported as a problem: %v", problems)
	}
}

// An app that does not serve a schema must still reconcile.
func TestValidateOptionFieldsSkipsWithoutSchema(t *testing.T) {
	opts := Options{Indexers: []commonv1alpha1.Indexer{{
		Name: "nzb", Implementation: "Newznab",
		Fields: []commonv1alpha1.ConfigField{{Name: "whatever"}},
	}}}

	if problems := ValidateOptionFields(context.Background(), schemaServer(t), "v3", opts); len(problems) != 0 {
		t.Errorf("missing schema should skip validation, got: %v", problems)
	}
}
