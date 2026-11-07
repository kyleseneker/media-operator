package export

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	indexersv1alpha1 "github.com/kyleseneker/media-operator/api/indexers/v1alpha1"
)

func TestFlareSolverrExportsSessionsWithoutASecret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","sessions":["prowlarr","compat"]}`))
	}))
	t.Cleanup(srv.Close)

	res, err := Run(context.Background(), Options{App: "flaresolverr", URL: srv.URL})
	require.NoError(t, err)
	cfg, ok := res.Config.(*indexersv1alpha1.FlareSolverrConfig)
	require.True(t, ok)
	assert.Equal(t, []indexersv1alpha1.FlareSolverrSession{{Name: "compat"}, {Name: "prowlarr"}}, cfg.Spec.Sessions)
	assert.Equal(t, srv.URL, cfg.Spec.Connection.URL)
	assert.Nil(t, res.Secret, "FlareSolverr has no credentials")
	out, err := res.YAML()
	require.NoError(t, err)
	assert.NotContains(t, string(out), "kind: Secret")
	assert.False(t, NeedsAPIKey("flaresolverr"))
	assert.True(t, NeedsAPIKey("sonarr"))
}
