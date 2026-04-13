# media-operator

**Stop configuring your media stack by hand.** Define Sonarr, Radarr, Jellyfin, and the rest as Kubernetes resources — rebuild from scratch with `kubectl apply`.

The *arr apps store everything in SQLite databases behind web UIs. If you lose your volumes, you lose every naming rule, download client, indexer, and quality profile you spent hours setting up. media-operator fixes this: your entire media stack configuration lives in git as Kubernetes CRDs, and the operator pushes it to your apps automatically.

```yaml
apiVersion: media-operator.dev/v1alpha1
kind: SonarrConfig
metadata:
  name: sonarr
spec:
  connection:
    url: http://sonarr:8989
    apiKeySecretRef:
      name: arr-secrets
      key: SONARR_API_KEY
  naming:
    renameEpisodes: true
    standardEpisodeFormat: "{Series TitleYear} - S{season:00}E{episode:00} - {Episode CleanTitle} [{Quality Full}]{-Release Group}"
    seriesFolderFormat: "{Series TitleYear} [tvdbid-{TvdbId}]"
  rootFolders:
    - path: /tv
  downloadClients:
    - name: qBittorrent
      protocol: torrent
      implementation: QBittorrent
      host: qbittorrent.media.svc.cluster.local
      port: 8080
      category: tv
```

## The Problem

You deploy your *arr apps declaratively with Helm or Kustomize, but the actual configuration — naming schemes, download clients, indexers, quality profiles, media management settings — is trapped in web UIs and SQLite databases. There's no way to:

- **Rebuild from scratch** without manually reconfiguring every app
- **Keep config in git** alongside the rest of your infrastructure
- **Prevent drift** when someone (or an upgrade) changes settings through the UI
- **Replicate your setup** to a second cluster or help a friend set up the same stack

Existing tools either only handle quality profiles (Recyclarr), are abandoned (Buildarr), or require Terraform (DevOpsArr). None of them are Kubernetes-native, and none continuously enforce your desired state.

## How media-operator Works

1. You apply a CR (e.g., `SonarrConfig`) describing your desired configuration
2. The operator resolves any referenced Kubernetes Secrets
3. It fetches the current configuration from the app's API
4. It diffs current state against desired state
5. If there's drift, it pushes the corrected configuration
6. It requeues and checks again every 5 minutes

For apps with first-time setup wizards (Jellyfin, Seerr), the operator detects a fresh install and runs the wizard automatically — no manual setup required.

## Supported Apps

### Servarr Family
| CRD | App | What it manages |
|-----|-----|----------------|
| `SonarrConfig` | Sonarr | Media management, naming, root folders, download clients, quality profiles, custom formats, indexers, notifications, import lists, tags, UI |
| `RadarrConfig` | Radarr | Same as Sonarr (movie variants) |
| `LidarrConfig` | Lidarr | Same as Sonarr (music variants) |
| `ReadarrConfig` | Readarr | Same as Sonarr (book variants) |
| `ProwlarrConfig` | Prowlarr | Indexers, app connections (Sonarr/Radarr sync), proxies (FlareSolverr) |
| `BazarrConfig` | Bazarr | Subtitle providers, languages, Sonarr/Radarr connections, subtitle sync |

### Download Clients
| CRD | App | What it manages |
|-----|-----|----------------|
| `QBittorrentConfig` | qBittorrent | Preferences, share ratios, connection limits, download categories |
| `SabnzbdConfig` | SABnzbd | Usenet servers, categories, folder paths |

### Media Servers
| CRD | App | What it manages |
|-----|-----|----------------|
| `JellyfinConfig` | Jellyfin | Libraries, hardware transcoding (VAAPI/QSV/NVENC), server settings, **first-time wizard** |
| `PlexConfig` | Plex | Libraries, transcoding, network, remote access, server settings |

### Request Management
| CRD | App | What it manages |
|-----|-----|----------------|
| `SeerrConfig` | Seerr/Jellyseerr | Sonarr/Radarr connections, Jellyfin or Plex auth, settings, notifications, **first-time setup** |
| `MaintainerrConfig` | Maintainerr | Plex/Sonarr/Radarr connections, media cleanup rules, collection handling |

### Transcoding
| CRD | App | What it manages |
|-----|-----|----------------|
| `TdarrConfig` | Tdarr | Libraries, transcode flows, worker limits |

### Automation
| CRD | App | What it manages |
|-----|-----|----------------|
| `AutobrrConfig` | Autobrr | Download clients, indexers, IRC networks, RSS/Torznab feeds, release filters with actions |

### Utilities
| CRD | App | What it manages |
|-----|-----|----------------|
| `FlareSolverrConfig` | FlareSolverr | Session management, health monitoring |

## Quick Start

### Install

media-operator is split into independent operators — install only what you need:

```bash
# Servarr family (Sonarr, Radarr, Lidarr, Readarr, Prowlarr, Bazarr)
helm install media-operator-servarr oci://ghcr.io/kyleseneker/media-operator/media-operator-servarr --namespace media

# Download clients (qBittorrent, SABnzbd)
helm install media-operator-downloads oci://ghcr.io/kyleseneker/media-operator/media-operator-downloads --namespace media

# Media servers (Jellyfin, Plex)
helm install media-operator-mediaservers oci://ghcr.io/kyleseneker/media-operator/media-operator-mediaservers --namespace media

# Request management (Seerr/Jellyseerr, Maintainerr)
helm install media-operator-requests oci://ghcr.io/kyleseneker/media-operator/media-operator-requests --namespace media

# Transcoding (Tdarr)
helm install media-operator-transcode oci://ghcr.io/kyleseneker/media-operator/media-operator-transcode --namespace media

# Automation (Autobrr)
helm install media-operator-automation oci://ghcr.io/kyleseneker/media-operator/media-operator-automation --namespace media

# Utilities (FlareSolverr)
helm install media-operator-utilities oci://ghcr.io/kyleseneker/media-operator/media-operator-utilities --namespace media
```

Each chart installs only its own CRDs and RBAC — no unused resources in your cluster.

### Create Secrets

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: arr-secrets
  namespace: media
type: Opaque
stringData:
  SONARR_API_KEY: "your-sonarr-api-key"
  RADARR_API_KEY: "your-radarr-api-key"
```

Works with any Secret source — ExternalSecrets, Sealed Secrets, or plain Secrets. The operator watches referenced Secrets and re-reconciles immediately when they change.

### Apply Configuration

```bash
kubectl apply -f sonarr.yaml
```

### Check Status

```bash
$ kubectl get sonarrconfigs -n media
NAME     SYNCED   READY   LAST SYNC              AGE
sonarr   True     True    2026-04-03T12:00:00Z   1h
```

## Full Stack Example

See [examples/jellyfin-stack.yaml](examples/jellyfin-stack.yaml) for a complete Sonarr + Radarr + Prowlarr + qBittorrent + Jellyfin setup, or [examples/plex-stack.yaml](examples/plex-stack.yaml) for the Plex-based variant.

More examples:
- [Minimal Sonarr](examples/minimal-sonarr.yaml) — simplest possible config to verify the operator works
- [Custom reconciliation interval](examples/custom-interval.yaml) — adjust how often drift is corrected
- [Complete CRD samples](config/samples/) — every field for every app

## Reconciliation Behavior

| Resource type | Behavior |
|--------------|----------|
| Settings (naming, media management, UI) | **Update-always** — drift is corrected every cycle |
| Root folders, Jellyfin libraries | **Create-only** — created if missing, never modified |
| Download clients, indexers, notifications | **Create-or-update** — matched by name |
| Tdarr libraries and flows | **Upsert** — matched by ID |
| Autobrr filters, download clients, IRC networks | **Create-or-update** — matched by name |
| FlareSolverr sessions | **Create-or-destroy** — sessions not in spec are removed |
| Maintainerr rules, app connections | **Create-or-update** — matched by name |

By default, the operator does **not** delete resources removed from the spec. Set `spec.reconcile.prune: true` to enable automatic deletion of unmanaged resources. Root folders and tags are never pruned.

## Metrics & Observability

Each operator binary exposes Prometheus metrics on `:8443/metrics` over HTTPS, protected by Kubernetes TokenReview authentication. Metrics are enabled by default in every Helm chart.

**Built-in metrics** (from controller-runtime):

- `controller_runtime_reconcile_total{controller,result}` — reconcile counts
- `controller_runtime_reconcile_time_seconds` — reconcile duration histogram
- `controller_runtime_reconcile_errors_total` — reconcile error counts
- `workqueue_*` — workqueue depth, add rate, retries

**Custom metrics** (prefixed `media_operator_`):

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `media_operator_app_api_request_duration_seconds` | Histogram | `app`, `method`, `outcome` | Latency of outbound HTTP calls to target apps |
| `media_operator_app_api_errors_total` | Counter | `app`, `status_class` | Non-2xx responses and network errors from target apps |
| `media_operator_resources_pruned_total` | Counter | `app`, `resource_type` | Resources deleted by the prune logic |
| `media_operator_managed_resources` | Gauge | `app`, `resource_type` | Number of resources declared per CR by type |

The `app` label is one of `sonarr`, `radarr`, `lidarr`, `readarr`, `prowlarr`, `bazarr`, `plex`, `jellyfin`, `seerr`, `maintainerr`, `qbittorrent`, `sabnzbd`, `tdarr`, `autobrr`, `flaresolverr`.

### Enabling ServiceMonitor

If you run prometheus-operator, enable the `ServiceMonitor` CR during install:

```bash
helm install media-operator-servarr chart/media-operator-servarr \
  --set metrics.serviceMonitor.enabled=true \
  --set metrics.serviceMonitor.labels.release=kube-prometheus-stack
```

The `ServiceMonitor` scrapes the operator's `/metrics` endpoint with a bearer token mounted from the pod's ServiceAccount, using `insecureSkipVerify: true` against the operator's self-signed cert. No cert-manager required.

### Example alerts

```promql
# Alert when pruning is happening at more than 0.5 resources/sec (1 every 2s) for 5 minutes.
rate(media_operator_resources_pruned_total[5m]) > 0.5

# Alert when a target app is returning 5xx errors.
rate(media_operator_app_api_errors_total{status_class="5xx"}[5m]) > 0

# Alert on elevated reconcile error rate.
rate(controller_runtime_reconcile_errors_total[5m]) > 0.1
```

### Disabling metrics

If you don't need metrics, disable them per chart:

```bash
helm install media-operator-servarr chart/media-operator-servarr --set metrics.enabled=false
```

This removes the metrics Service, RBAC, and ServiceMonitor, and passes `--metrics-bind-address=0` to the operator to disable the endpoint entirely.

## Works Great With

media-operator manages application *configuration*, not deployment. Pair it with your preferred deployment tool:

- **Helm** — [bjw-s common library chart](https://github.com/bjw-s/helm-charts) for deploying *arr containers
- **Flux / ArgoCD** — GitOps the CRDs alongside your HelmReleases
- **ExternalSecrets / Sealed Secrets** — feed API keys to media-operator without plain-text Secrets in git

## Development

```bash
# Prerequisites: Go 1.25+, kubebuilder

BINARY=servarr make run   # Run a specific operator locally against your cluster
make build                # Build all 7 binaries
make test                 # Run tests
make generate manifests   # Regenerate CRDs after modifying types
```

See [CONTRIBUTING.md](CONTRIBUTING.md) for development guidelines.

## License

Apache License 2.0 — see [LICENSE](LICENSE).
