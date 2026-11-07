package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func runCLI(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), args, &stdout, &stderr)
	return stdout.String(), stderr.String(), err
}

func TestRunRejectsAMissingOrUnknownCommand(t *testing.T) {
	_, stderr, err := runCLI(t)
	require.EqualError(t, err, "no command given")
	assert.Contains(t, stderr, "Usage:")

	_, _, err = runCLI(t, "import")
	require.EqualError(t, err, `unknown command "import"`)
}

func TestVersionPrintsTheBuild(t *testing.T) {
	stdout, _, err := runCLI(t, "version")
	require.NoError(t, err)
	assert.Equal(t, "dev\n", stdout)
}

func TestExportRequiresAnAppURLAndKey(t *testing.T) {
	t.Setenv("MEDIA_OPERATOR_API_KEY", "")
	_, _, err := runCLI(t, "export")
	require.EqualError(t, err, "export needs an app")
	_, _, err = runCLI(t, "export", "--url", "http://sonarr")
	require.EqualError(t, err, "export needs an app")
	_, _, err = runCLI(t, "export", "sonarr")
	require.EqualError(t, err, "--url is required")
	_, _, err = runCLI(t, "export", "sonarr", "--url", "http://sonarr")
	require.EqualError(t, err, "--api-key or $MEDIA_OPERATOR_API_KEY is required")
	_, _, err = runCLI(t, "export", "sonarr", "--url", "http://sonarr", "--api-key", "k", "extra")
	require.EqualError(t, err, "unexpected arguments: extra")
}

func TestExportRequiresALoginForApps(t *testing.T) {
	t.Setenv("MEDIA_OPERATOR_USERNAME", "")
	t.Setenv("MEDIA_OPERATOR_PASSWORD", "")
	_, _, err := runCLI(t, "export", "qbittorrent", "--url", "http://qbittorrent", "--username", "admin")
	require.EqualError(t, err,
		"qbittorrent needs --username and --password, or $MEDIA_OPERATOR_USERNAME and $MEDIA_OPERATOR_PASSWORD")
}

func TestExportRequiresATokenForPlex(t *testing.T) {
	t.Setenv("MEDIA_OPERATOR_TOKEN", "")
	_, _, err := runCLI(t, "export", "plex", "--url", "http://plex")
	require.EqualError(t, err, "plex needs --token or $MEDIA_OPERATOR_TOKEN")
}

func TestExportPrintsTheSecretAndConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/config/") {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_, _ = w.Write([]byte(`[]`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("MEDIA_OPERATOR_API_KEY", "from-env")

	stdout, stderr, err := runCLI(t, "export", "sonarr", "--url", srv.URL, "--namespace", "media")
	require.NoError(t, err)
	assert.Contains(t, stdout, "kind: Secret")
	assert.Contains(t, stdout, "api-key: from-env")
	assert.Contains(t, stdout, "kind: SonarrConfig")
	assert.Contains(t, stdout, "namespace: media")
	assert.Empty(t, stderr, "nothing to fill in when the app has no credentials")
}
