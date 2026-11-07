//go:build compat

package compat

// Application versions the suite runs against; README's supported apps table
// lists the same versions. Renovate updates each pin, and the compat workflow
// then proves the new version before it merges.
const (
	// renovate: datasource=docker
	sonarrImage = "lscr.io/linuxserver/sonarr:4.0.20.3014-ls2"
	// renovate: datasource=docker
	radarrImage = "lscr.io/linuxserver/radarr:6.4.4.10685-ls325"
	// renovate: datasource=docker
	lidarrImage = "lscr.io/linuxserver/lidarr:3.1.6.5079-ls47"
	// renovate: datasource=docker
	readarrImage = "lscr.io/linuxserver/readarr:develop-0.4.18.2805-ls157"
	// renovate: datasource=docker
	prowlarrImage = "lscr.io/linuxserver/prowlarr:2.7.0.5723-ls167"
	// renovate: datasource=docker
	flaresolverrImage = "ghcr.io/flaresolverr/flaresolverr:v3.5.2"
	// renovate: datasource=docker
	autobrrImage = "ghcr.io/autobrr/autobrr:v1.90.0"
	// renovate: datasource=docker
	bazarrImage = "lscr.io/linuxserver/bazarr:v1.6.2-ls371"
	// renovate: datasource=docker
	jellyfinImage = "docker.io/jellyfin/jellyfin:12.4"
	// renovate: datasource=docker
	seerrImage = "ghcr.io/seerr-team/seerr:v3.5.1"
	// renovate: datasource=docker
	maintainerrImage = "ghcr.io/maintainerr/maintainerr:3.32.0"
	// renovate: datasource=docker
	tdarrImage = "ghcr.io/haveagitgat/tdarr:3.06.00"
	// renovate: datasource=docker
	sabnzbdImage = "lscr.io/linuxserver/sabnzbd:5.1.3-ls280"
	// renovate: datasource=docker
	plexImage = "docker.io/plexinc/pms-docker:1.43.5.11079-b3b13e506"
	// renovate: datasource=docker
	qbittorrentImage = "lscr.io/linuxserver/qbittorrent:5.3.0_v2.1.2-ls486"
)
