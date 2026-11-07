package export

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	mediaserversv1alpha1 "github.com/kyleseneker/media-operator/api/mediaservers/v1alpha1"
)

func TestJellyfinExportsSettingsLibrariesAndTheAdminUser(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/Users/AuthenticateByName":
			_, _ = w.Write([]byte(`{"AccessToken":"token"}`))
		case "/System/Configuration":
			_, _ = w.Write([]byte(`{"ServerName":"home","LogFileRetentionDays":3,"NotInTheCRD":true}`))
		case "/System/Configuration/encoding":
			_, _ = w.Write([]byte(`{"HardwareAccelerationType":"vaapi","H264Crf":23,"HardwareDecodingCodecs":["h264","hevc"]}`))
		case "/Library/VirtualFolders":
			_, _ = w.Write([]byte(`[
				{"Name":"Movies","CollectionType":"movies","Locations":["/media/movies"],
					"LibraryOptions":{"EnableRealtimeMonitor":true,"AutomaticRefreshIntervalDays":30}},
				{"Name":"Everything","Locations":["/media/other"],"LibraryOptions":{}}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	res, err := Run(context.Background(), Options{App: "jellyfin", URL: srv.URL, Username: "admin", Password: "pw"})
	require.NoError(t, err)
	cfg, ok := res.Config.(*mediaserversv1alpha1.JellyfinConfig)
	require.True(t, ok)
	assert.Equal(t, "home", cfg.Spec.Server.ServerName)
	assert.Equal(t, 3, *cfg.Spec.Server.LogFileRetentionDays)
	assert.Equal(t, "vaapi", cfg.Spec.Encoding.HardwareAccelerationType)
	assert.Equal(t, []string{"h264", "hevc"}, cfg.Spec.Encoding.HardwareDecodingCodecs)
	require.Len(t, cfg.Spec.Libraries, 2)
	assert.Equal(t, []string{"/media/movies"}, cfg.Spec.Libraries[0].Paths)
	assert.True(t, *cfg.Spec.Libraries[0].EnableRealtimeMonitor)
	assert.Equal(t, 30, *cfg.Spec.Libraries[0].AutomaticRefreshIntervalDays)
	assert.Equal(t, "mixed", cfg.Spec.Libraries[1].CollectionType)
	assert.Equal(t, "username", cfg.Spec.AdminUser.UsernameSecretRef.Key)
	assert.Equal(t, map[string]string{"username": "admin", "password": "pw"}, res.Secret.StringData)
}
