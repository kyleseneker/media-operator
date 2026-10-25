package pvr

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	common "github.com/kyleseneker/media-operator/api/common/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/engine"
	"github.com/stretchr/testify/require"
)

// rebuildAPI persists writes and assigns IDs like a newly initialized app. This
// exercises ordering and convergence, not compatibility with a real Servarr API.
type rebuildAPI struct {
	mu     sync.Mutex
	items  map[string][]map[string]any
	writes []string
	nextID int
	fail   string
}

func (a *rebuildAPI) client(t *testing.T) *engine.HTTPClient {
	t.Helper()
	if a.items == nil {
		a.items = map[string][]map[string]any{}
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		defer a.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		resource := strings.TrimPrefix(r.URL.Path, "/api/v3/")
		if r.Method == http.MethodGet {
			if resource == a.fail {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			items := a.items[resource]
			if items == nil {
				items = []map[string]any{}
			}
			_ = json.NewEncoder(w).Encode(items)
			return
		}
		a.writes = append(a.writes, r.Method+" "+resource)
		if resource == a.fail {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		var body map[string]any
		if r.Method != http.MethodDelete {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		switch r.Method {
		case http.MethodPost:
			a.nextID++
			body["id"] = float64(a.nextID)
			a.items[resource] = append(a.items[resource], body)
		case http.MethodPut, http.MethodDelete:
			id, _ := strconv.Atoi(path.Base(resource))
			resource = path.Dir(resource)
			for i, item := range a.items[resource] {
				if item["id"] == float64(id) {
					if r.Method == http.MethodPut {
						a.items[resource][i] = body
					} else {
						a.items[resource] = slices.Delete(a.items[resource], i, i+1)
					}
					break
				}
			}
		}
		_ = json.NewEncoder(w).Encode(body)
	}))
	t.Cleanup(srv.Close)
	hc, err := engine.NewHTTPClient(srv.URL, engine.AuthNone, engine.WithTransport(&http.Transport{}))
	require.NoError(t, err)
	return hc
}

func TestRebuildResolvesReferencesAndConverges(t *testing.T) {
	for _, firstID := range []int{100, 800} {
		t.Run(fmt.Sprint(firstID), func(t *testing.T) {
			app := &rebuildAPI{nextID: firstID}
			hc := app.client(t)
			opts := Options{
				Tags:            []common.Tag{{Label: "tv"}},
				DownloadClients: []common.DownloadClient{{Name: "downloads", TagLabels: []string{"tv"}, Tags: []int{7}}},
				Indexers:        []common.Indexer{{Name: "indexer", TagLabels: []string{"tv"}}},
				Notifications:   []common.Notification{{Name: "notify", TagLabels: []string{"tv"}}},
				CustomFormats:   []common.CustomFormat{{Name: "preferred"}},
				QualityProfiles: []common.QualityProfile{{Name: "HD", FormatItems: []common.QualityProfileFormatItem{{Name: "preferred", Score: 100}}}},
				ImportLists:     []common.ImportList{{Name: "list", QualityProfileName: "HD", TagLabels: []string{"tv"}}},
			}
			result, err := Reconcile(context.Background(), hc, "v3", struct{}{}, opts, engine.SyncPolicy{}, nil)
			require.NoError(t, err)
			require.Empty(t, result.Errors)
			tagID := app.items["tag"][0]["id"]
			for _, kind := range []string{"downloadclient", "indexer", "notification", "importlist"} {
				require.Equal(t, []any{tagID}, app.items[kind][0]["tags"], kind)
			}
			require.Equal(t, app.items["qualityprofile"][0]["id"], app.items["importlist"][0]["qualityProfileId"])
			require.Equal(t, []int{7}, opts.DownloadClients[0].Tags, "must not mutate spec")
			writes := len(app.writes)
			second, err := Reconcile(context.Background(), hc, "v3", struct{}{}, opts, engine.SyncPolicy{}, result.Managed)
			require.NoError(t, err)
			require.Empty(t, second.Errors)
			require.Len(t, app.writes, writes, "second pass must not write")
			require.Equal(t, result.Managed, second.Managed, "ownership snapshots must not accumulate duplicates")
		})
	}
}

func TestMissingTagBlocksDependentWritesAndRetainsOwnership(t *testing.T) {
	app := &rebuildAPI{}
	hc := app.client(t)
	opts := Options{Tags: []common.Tag{{Label: "created"}}, DownloadClients: []common.DownloadClient{{Name: "downloads", TagLabels: []string{"missing"}}}}
	result, err := Reconcile(context.Background(), hc, "v3", struct{}{}, opts, engine.SyncPolicy{Prune: true}, map[string][]string{"downloadClients": {"old"}})
	require.NoError(t, err)
	require.NotEmpty(t, result.Errors)
	require.Equal(t, []string{"created"}, result.Managed["tags"])
	require.Equal(t, []string{"old"}, result.Managed["downloadClients"])
	require.Equal(t, []string{"POST tag"}, app.writes)
}

func TestObserveFreshReferencesDoesNotWrite(t *testing.T) {
	app := &rebuildAPI{}
	hc := app.client(t)
	opts := Options{Tags: []common.Tag{{Label: "tv"}}, DownloadClients: []common.DownloadClient{{Name: "downloads", TagLabels: []string{"tv"}}}}
	result, err := Reconcile(context.Background(), hc, "v3", struct{}{}, opts, engine.SyncPolicy{Observe: true}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, result.Errors)
	require.Empty(t, app.writes)
	require.Empty(t, result.Managed)
}

func TestReferenceFailuresNeverDropAssociations(t *testing.T) {
	for _, tc := range []struct {
		name  string
		items []map[string]any
		fail  bool
	}{
		{name: "missing"},
		{name: "ambiguous", items: []map[string]any{{"label": "tv", "id": 1}, {"label": "tv", "id": 2}}},
		{name: "invalid id", items: []map[string]any{{"label": "tv", "id": 0}}},
		{name: "lookup error", fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := &rebuildAPI{items: map[string][]map[string]any{"tag": tc.items}}
			if tc.fail {
				app.fail = "tag"
			}
			hc := app.client(t)
			result, err := Reconcile(context.Background(), hc, "v3", struct{}{}, Options{Indexers: []common.Indexer{{Name: "indexer", TagLabels: []string{"tv"}}}}, engine.SyncPolicy{}, nil)
			require.NoError(t, err)
			require.NotEmpty(t, result.Errors)
			require.Empty(t, app.writes)
		})
	}
}

func TestMissingQualityReferencesBlockWrites(t *testing.T) {
	for _, opts := range []Options{
		{QualityProfiles: []common.QualityProfile{{Name: "HD", FormatItems: []common.QualityProfileFormatItem{{Name: "missing"}}}}},
		{ImportLists: []common.ImportList{{Name: "list", QualityProfileName: "missing"}}},
	} {
		app := &rebuildAPI{}
		hc := app.client(t)
		result, err := Reconcile(context.Background(), hc, "v3", struct{}{}, opts, engine.SyncPolicy{}, nil)
		require.NoError(t, err)
		require.NotEmpty(t, result.Errors)
		require.Empty(t, app.writes)
	}
}

func TestRemotePathMappingLifecycle(t *testing.T) {
	app := &rebuildAPI{}
	hc := app.client(t)
	ctx := context.Background()
	opts := Options{RemotePathMappings: []common.RemotePathMapping{
		{Host: "seedbox-a", RemotePath: "/downloads", LocalPath: "/data/a"},
		{Host: "seedbox-b", RemotePath: "/downloads", LocalPath: "/data/b"},
	}}
	result, err := Reconcile(ctx, hc, "v3", struct{}{}, opts, engine.SyncPolicy{}, nil)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.Len(t, app.items["remotepathmapping"], 2)
	require.Len(t, result.Managed["remotePathMappings"], 2)
	writes := len(app.writes)
	result, err = Reconcile(ctx, hc, "v3", struct{}{}, opts, engine.SyncPolicy{}, result.Managed)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.Len(t, app.writes, writes)
	opts.RemotePathMappings[0].LocalPath = "/data/new"
	observed, err := Reconcile(ctx, hc, "v3", struct{}{}, opts, engine.SyncPolicy{Observe: true}, result.Managed)
	require.NoError(t, err)
	require.Empty(t, observed.Errors)
	require.Len(t, app.writes, writes)
	result, err = Reconcile(ctx, hc, "v3", struct{}{}, opts, engine.SyncPolicy{}, result.Managed)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.Equal(t, "/data/new/", app.items["remotepathmapping"][0]["localPath"])
	// An unrelated hand-created mapping shares a path with the managed entries.
	app.items["remotepathmapping"] = append(app.items["remotepathmapping"], map[string]any{"id": float64(900), "host": "manual", "remotePath": "/downloads/", "localPath": "/data/manual/"})
	opts.RemotePathMappings = opts.RemotePathMappings[:1]
	// A later quality-profile stage must not resurrect ownership removed by prune.
	opts.QualityProfiles = []common.QualityProfile{{Name: "HD"}}
	result, err = Reconcile(ctx, hc, "v3", struct{}{}, opts, engine.SyncPolicy{Prune: true}, result.Managed)
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.Len(t, result.Managed["remotePathMappings"], 1)
	require.Len(t, app.items["remotepathmapping"], 2)
	_, err = engine.DeleteManagedResources(ctx, hc, Definition("v3"), result.Managed)
	require.NoError(t, err)
	require.Len(t, app.items["remotepathmapping"], 1)
	require.Equal(t, "manual", app.items["remotepathmapping"][0]["host"])
}

func TestRemotePathMappingWindowsIdentity(t *testing.T) {
	a := map[string]any{"host": "seedbox", "remotePath": `C:\Downloads`}
	b := map[string]any{"host": "seedbox", "remotePath": `C:\Downloads\`}
	require.Equal(t, remotePathMappingKey(a), remotePathMappingKey(b))
	require.Equal(t, `C:\Downloads\`, directoryPath(`C:\Downloads`))
	// JSON tuple encoding avoids separator collisions between arbitrary hosts/paths.
	require.NotEqual(t, remotePathMappingKey(map[string]any{"host": "a/b", "remotePath": "c"}), remotePathMappingKey(map[string]any{"host": "a", "remotePath": "b/c"}))
}

func TestDuplicateNormalizedMappingBlocksWrites(t *testing.T) {
	app := &rebuildAPI{}
	hc := app.client(t)
	opts := Options{RemotePathMappings: []common.RemotePathMapping{
		{Host: "seedbox", RemotePath: "/downloads", LocalPath: "/data/a"},
		{Host: "seedbox", RemotePath: "/downloads/", LocalPath: "/data/b"},
	}}
	result, err := Reconcile(context.Background(), hc, "v3", struct{}{}, opts, engine.SyncPolicy{}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, result.Errors)
	require.Empty(t, app.writes)
}
