# Contributing to media-operator

## Getting started

1. Fork and clone: `git clone https://github.com/<your-username>/media-operator`
2. Branch: `git checkout -b my-feature`
3. Make your changes
4. `make test lint verify-crds`
5. Push and open a PR

You'll need Go 1.25+, kubebuilder, a cluster (kind is fine), and access to
whichever app you're working on. Most of the interesting behavior only shows up
against a real Sonarr or Jellyfin, so it helps to have one running.

```bash
BINARY=pvr make run          # run one operator against your current kube context
make build                   # all nine binaries
make test
make manifests generate      # after editing anything in api/
make sync-crds               # copy generated CRDs into the charts
make lint
```

`BINARY` is one of: `pvr`, `indexers`, `subtitles`, `downloads`, `mediaservers`,
`requests`, `curation`, `transcode`, `automation`.

## Layout

Nine operators, grouped by what the app does rather than by vendor family. Each
has a `cmd/` entrypoint, an API package, a controller package, and a Helm chart,
all named the same way.

| Binary | Apps | API package |
|--------|------|-------------|
| `media-operator-pvr` | Sonarr, Radarr, Lidarr, Readarr | `api/pvr/v1alpha1/` |
| `media-operator-indexers` | Prowlarr, FlareSolverr | `api/indexers/v1alpha1/` |
| `media-operator-subtitles` | Bazarr | `api/subtitles/v1alpha1/` |
| `media-operator-downloads` | qBittorrent, SABnzbd | `api/downloads/v1alpha1/` |
| `media-operator-mediaservers` | Jellyfin, Plex | `api/mediaservers/v1alpha1/` |
| `media-operator-requests` | Seerr/Jellyseerr | `api/requests/v1alpha1/` |
| `media-operator-curation` | Maintainerr | `api/curation/v1alpha1/` |
| `media-operator-transcode` | Tdarr | `api/transcode/v1alpha1/` |
| `media-operator-automation` | Autobrr | `api/automation/v1alpha1/` |
