//go:build compat

package compat

import (
	"net/http"
	"testing"
)

// The applications test indexers, notifications and lists when they are saved,
// so declared endpoints must answer. These stand in for third-party services.
const torznabCaps = `<?xml version="1.0" encoding="UTF-8"?>
<caps>
  <server title="compat"/>
  <limits max="100" default="100"/>
  <searching>
    <search available="yes" supportedParams="q"/>
    <tv-search available="yes" supportedParams="q,season,ep"/>
    <movie-search available="yes" supportedParams="q"/>
    <music-search available="yes" supportedParams="q"/>
    <audio-search available="yes" supportedParams="q"/>
    <book-search available="yes" supportedParams="q"/>
  </searching>
  <categories>
    <category id="2000" name="Movies"><subcat id="2040" name="Movies/HD"/></category>
    <category id="3000" name="Audio"><subcat id="3040" name="Audio/Lossless"/></category>
    <category id="5000" name="TV"><subcat id="5040" name="TV/HD"/></category>
    <category id="7000" name="Books"><subcat id="7020" name="Books/EBook"/></category>
  </categories>
</caps>`

// One release in every category, so each application's test search finds a result.
const torznabFeed = `<?xml version="1.0" encoding="UTF-8"?>
<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed">
  <channel>
    <title>compat</title>
    <item>
      <title>Compat.Release.S01E01.1080p.WEB-DL.H264-GRP</title>
      <guid>compat-1</guid>
      <link>http://compat.invalid/download/1.torrent</link>
      <comments>http://compat.invalid/details/1</comments>
      <pubDate>Mon, 02 Nov 2026 12:00:00 +0000</pubDate>
      <size>1073741824</size>
      <enclosure url="http://compat.invalid/download/1.torrent" length="1073741824" type="application/x-bittorrent"/>
      <torznab:attr name="category" value="2000"/>
      <torznab:attr name="category" value="3000"/>
      <torznab:attr name="category" value="5000"/>
      <torznab:attr name="category" value="7000"/>
      <torznab:attr name="seeders" value="10"/>
      <torznab:attr name="peers" value="12"/>
    </item>
  </channel>
</rss>`

// startFakes serves the third-party endpoints fixtures point at, on ${FAKES}.
func startFakes(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/torznab/api", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		if r.URL.Query().Get("t") == "caps" {
			_, _ = w.Write([]byte(torznabCaps))
			return
		}
		_, _ = w.Write([]byte(torznabFeed))
	})
	mux.HandleFunc("/webhook", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	// Custom import lists, one item each: some apps reject a list that returns nothing.
	lists := map[string]string{
		"/lists/sonarr": `[{"title":"Breaking Bad","tvdbId":81189}]`,
		"/lists/radarr": `[{"id":603,"title":"The Matrix","imdb_id":"tt0133093"}]`,
	}
	for path, body := range lists {
		mux.HandleFunc(path, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(body))
		})
	}
	return serveOnHostIP(t, mux).URL
}
