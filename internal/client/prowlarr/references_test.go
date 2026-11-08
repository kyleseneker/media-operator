package prowlarr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	indexersv1alpha1 "github.com/kyleseneker/media-operator/api/indexers/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/engine"
)

func referenceServer(t *testing.T, lists map[string][]map[string]any) *engine.HTTPClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		items, ok := lists[r.URL.Path]
		if !ok {
			items = []map[string]any{}
		}
		_ = json.NewEncoder(w).Encode(items)
	}))
	t.Cleanup(srv.Close)

	hc, err := engine.NewHTTPClient(srv.URL, engine.AuthAPIKey,
		engine.WithAPIKey("k"), engine.WithAPIKeyHeader("X-Api-Key"),
		engine.WithTransport(&http.Transport{}))
	require.NoError(t, err)
	return hc
}

func TestResolveReferencesFollowsRecreatedAppProfile(t *testing.T) {
	lists := map[string][]map[string]any{
		"/api/v1/appprofile": {{"id": 3, "name": "Standard"}},
	}
	hc := referenceServer(t, lists)
	spec := indexersv1alpha1.ProwlarrConfigSpec{
		Indexers: []indexersv1alpha1.ProwlarrIndexer{{Name: "idx", AppProfileName: "Standard"}},
	}

	first, _, err := ResolveReferences(context.Background(), hc, spec)
	require.NoError(t, err)
	require.NotNil(t, first.Indexers[0].AppProfileId)
	assert.Equal(t, 3, *first.Indexers[0].AppProfileId)

	lists["/api/v1/appprofile"] = []map[string]any{{"id": 7, "name": "Standard"}}

	second, _, err := ResolveReferences(context.Background(), hc, spec)
	require.NoError(t, err)
	require.NotNil(t, second.Indexers[0].AppProfileId)
	assert.Equal(t, 7, *second.Indexers[0].AppProfileId)
	assert.Nil(t, spec.Indexers[0].AppProfileId, "the input spec must not carry application IDs")
}

func TestResolveReferencesRejectsMissingAppProfile(t *testing.T) {
	hc := referenceServer(t, map[string][]map[string]any{
		"/api/v1/appprofile": {{"id": 3, "name": "Standard"}},
	})
	spec := indexersv1alpha1.ProwlarrConfigSpec{
		Indexers: []indexersv1alpha1.ProwlarrIndexer{{Name: "idx", AppProfileName: "Gone"}},
	}

	_, _, err := ResolveReferences(context.Background(), hc, spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "indexers(idx): appProfileName")
}

func TestResolveReferencesRejectsMissingTag(t *testing.T) {
	hc := referenceServer(t, map[string][]map[string]any{
		"/api/v1/tag": {{"id": 1, "label": "known"}},
	})
	spec := indexersv1alpha1.ProwlarrConfigSpec{
		Indexers: []indexersv1alpha1.ProwlarrIndexer{{Name: "idx", Tags: []string{"known", "unknown"}}},
	}

	_, _, err := ResolveReferences(context.Background(), hc, spec)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "indexers(idx): tag")
}
