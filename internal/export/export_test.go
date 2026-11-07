package export

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRunRejectsAnUnsupportedApp(t *testing.T) {
	_, err := Run(context.Background(), Options{App: "nope", URL: "http://x"})
	require.ErrorContains(t, err, `unsupported app "nope"; supported: bazarr, flaresolverr, lidarr, prowlarr, radarr, readarr, sonarr`)
}

func TestRunRejectsAnExcludeTheSpecLacks(t *testing.T) {
	_, err := Run(context.Background(), Options{App: "sonarr", URL: "http://x", Exclude: []string{"indexer"}})
	require.ErrorContains(t, err, `cannot exclude "indexer"`)
}

func TestRunNamesAndPointsTheConfig(t *testing.T) {
	cfg, res := exportSonarr(t, Options{Name: "tv", Namespace: "media", ConnectionURL: "http://sonarr.media:8989"})
	assert.Equal(t, "tv", cfg.Name)
	assert.Equal(t, "media", cfg.Namespace)
	assert.Equal(t, "SonarrConfig", cfg.Kind)
	assert.Equal(t, "http://sonarr.media:8989", cfg.Spec.Connection.URL)
	assert.Equal(t, "tv-credentials", cfg.Spec.Connection.APIKeySecretRef.Name)
	assert.Equal(t, "tv-credentials", res.Secret.Name)
	assert.Equal(t, "media", res.Secret.Namespace)
}

func TestRunExcludeDropsTheSectionAndItsCredentials(t *testing.T) {
	cfg, res := exportSonarr(t, Options{Exclude: []string{"indexers", "importLists"}})
	assert.Empty(t, cfg.Spec.Indexers)
	assert.Empty(t, cfg.Spec.ImportLists)
	assert.NotContains(t, res.Secret.StringData, "indexer-torznab-apikey")
	assert.Equal(t, []string{"download-client-qbit-password"}, res.Unfilled)
}

func TestResultYAMLHasNoServerFields(t *testing.T) {
	_, res := exportSonarr(t, Options{})
	out, err := res.YAML()
	require.NoError(t, err)
	docs := strings.Split(string(out), "---\n")
	require.Len(t, docs, 2)
	assert.Contains(t, docs[0], "kind: Secret")
	assert.Contains(t, docs[1], "kind: SonarrConfig")
	assert.NotContains(t, string(out), "creationTimestamp")
	assert.NotContains(t, string(out), "status:")
}

func TestCredentialKeysAreValidAndUnique(t *testing.T) {
	c := &credentials{secret: "s", values: map[string]string{}}
	assert.Equal(t, "download-client-my-qbit-password", c.ref("", "download-client", "My qBit!", "password")["key"])
	assert.Equal(t, "download-client-my-qbit-password-2", c.ref("", "download-client", "my qbit", "password")["key"])
}
