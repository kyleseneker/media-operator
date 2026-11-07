# media-operator

[![CI](https://github.com/kyleseneker/media-operator/actions/workflows/ci.yml/badge.svg)](https://github.com/kyleseneker/media-operator/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/kyleseneker/media-operator)](https://github.com/kyleseneker/media-operator/releases)

Kubernetes operators that configure Sonarr, Radarr, Prowlarr, Jellyfin, Plex
and other self-hosted media apps from custom resources.

## Features

- Settings, download clients, indexers, quality profiles, notifications and other app configuration as YAML
- Drift correction on every reconcile, or an observe mode that only reports differences
- Credentials read from Kubernetes Secrets
- Optional pruning, limited to what the operator created
- `media-operator export` to generate config from an app you already run
- Tested in CI against each app running in a container

## Supported apps

| App | Resource | Chart | Tested version |
| --- | --- | --- | --- |
| Sonarr | `SonarrConfig` | `media-operator-pvr` | 4.0.20 |
| Radarr | `RadarrConfig` | `media-operator-pvr` | 6.4.4 |
| Lidarr | `LidarrConfig` | `media-operator-pvr` | 3.1.6 |
| Readarr | `ReadarrConfig` | `media-operator-pvr` | 0.4.18 |
| Prowlarr | `ProwlarrConfig` | `media-operator-indexers` | 2.7.0 |
| FlareSolverr | `FlareSolverrConfig` | `media-operator-indexers` | 3.5.2 |
| Bazarr | `BazarrConfig` | `media-operator-subtitles` | 1.6.2 |
| qBittorrent | `QBittorrentConfig` | `media-operator-downloads` | 5.3.0 |
| SABnzbd | `SabnzbdConfig` | `media-operator-downloads` | 5.1.3 |
| Jellyfin | `JellyfinConfig` | `media-operator-mediaservers` | 12.4 |
| Plex | `PlexConfig` | `media-operator-mediaservers` | 1.43.5 |
| Seerr | `SeerrConfig` | `media-operator-requests` | 3.5.1 |
| Maintainerr | `MaintainerrConfig` | `media-operator-curation` | 3.32.0 |
| Tdarr | `TdarrConfig` | `media-operator-transcode` | 3.06.00 |
| Autobrr | `AutobrrConfig` | `media-operator-automation` | 1.90.0 |

## Getting started

Install the chart for the apps you run:

```bash
helm install media-operator-pvr \
  oci://ghcr.io/kyleseneker/media-operator/media-operator-pvr \
  --namespace media
```

Then apply a config. [examples/minimal-sonarr.yaml](examples/minimal-sonarr.yaml)
adds a root folder to Sonarr:

```bash
kubectl apply -f examples/minimal-sonarr.yaml
kubectl get sonarrconfig -n media
```

To start from an existing setup instead, see
[Exporting an existing app](docs/export.md).

## Documentation

- [User guide](docs/guide.md)
- [Exporting an existing app](docs/export.md)
- [Rebuilding apps](docs/rebuilding.md)
- [Examples](examples/) and [one sample per resource](config/samples/)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). Report security issues as described in
[SECURITY.md](SECURITY.md).

## License

[Apache 2.0](LICENSE)
