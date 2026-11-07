//go:build compat

package compat

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kyleseneker/media-operator/internal/engine"
)

func randomKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// linuxserver images run the application as abc with these IDs.
var linuxserverEnv = map[string]string{"PUID": "1000", "PGID": "1000", "TZ": "Etc/UTC"}

func withEnv(base map[string]string, extra map[string]string) map[string]string {
	out := map[string]string{}
	maps.Copy(out, base)
	maps.Copy(out, extra)
	return out
}

// directClient talks to an application without the operator, for verification.
func directClient(t *testing.T, base, apiKey string) *engine.HTTPClient {
	t.Helper()
	hc, err := engine.NewHTTPClient(base, engine.AuthAPIKey, engine.WithAPIKey(apiKey), engine.WithAppLabel("compat"))
	require.NoError(t, err)
	return hc
}

// startPVR runs Sonarr, Radarr, Lidarr, Readarr or Prowlarr with a known API
// key, supplied through the <APP>__AUTH__APIKEY environment variable they read.
func startPVR(t *testing.T, app, image string, port int, apiVersion string) (*container, string) {
	t.Helper()
	key := randomKey()
	c := startContainer(t, containerSpec{
		name: app, image: image, port: port,
		env: withEnv(linuxserverEnv, map[string]string{strings.ToUpper(app) + "__AUTH__APIKEY": key}),
	})
	hc := directClient(t, c.url(), key)
	waitFor(t, app, 3*time.Minute, func() error {
		return hc.Ping(context.Background(), "/api/"+apiVersion+"/system/status")
	})
	return c, key
}

const qbittorrentUser = "admin"

// startQBittorrent seeds the WebUI credentials so the first boot needs no
// temporary password from the logs, and disables Host header validation because
// the operator reaches it through the recording proxy.
func startQBittorrent(t *testing.T) (*container, string) {
	t.Helper()
	password := randomKey()
	salt := make([]byte, 16)
	_, _ = rand.Read(salt)
	hash, err := pbkdf2.Key(sha512.New, password, salt, 100000, 64)
	require.NoError(t, err)
	conf := fmt.Sprintf(`[LegalNotice]
Accepted=true

[BitTorrent]
Session\DefaultSavePath=/downloads/
Session\TempPath=/downloads/incomplete/

[Preferences]
WebUI\Address=*
WebUI\Port=8080
WebUI\Username=%s
WebUI\Password_PBKDF2="@ByteArray(%s:%s)"
WebUI\HostHeaderValidation=false
WebUI\LocalHostAuth=false
`, qbittorrentUser, base64.StdEncoding.EncodeToString(salt), base64.StdEncoding.EncodeToString(hash))

	c := startContainer(t, containerSpec{
		name: "qbittorrent", image: qbittorrentImage, port: 8080,
		env:   withEnv(linuxserverEnv, map[string]string{"WEBUI_PORT": "8080"}),
		files: map[string]string{"/config/qBittorrent/qBittorrent.conf": conf},
	})
	waitFor(t, "qbittorrent", 2*time.Minute, func() error {
		form := url.Values{"username": {qbittorrentUser}, "password": {password}}
		resp, err := http.PostForm(c.url()+"/api/v2/auth/login", form)
		if err != nil {
			return err
		}
		defer func() { _ = resp.Body.Close() }()
		body, _ := io.ReadAll(resp.Body)
		// 5.x answers 204 with no body; older releases answer 200 "Ok.".
		if text := strings.TrimSpace(string(body)); resp.StatusCode/100 != 2 || (text != "" && text != "Ok.") {
			return fmt.Errorf("login: %d %s", resp.StatusCode, body)
		}
		return nil
	})
	return c, password
}
