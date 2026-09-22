# media-operator

[![CI](https://github.com/kyleseneker/media-operator/actions/workflows/ci.yml/badge.svg)](https://github.com/kyleseneker/media-operator/actions/workflows/ci.yml)

Manage Sonarr, Radarr, Jellyfin, Plex and friends as Kubernetes resources.

These apps all have a backup button and it works fine for putting one instance
back the way it was. What it doesn't give you is config you can review in a pull
request, apply to a second instance, or hold in place when someone changes it in
the UI. This operator reads config from a CR, diffs it against what the app's API
reports, and pushes the difference. Every 5 minutes, forever.

```yaml
apiVersion: media-operator.dev/v1alpha1
kind: SonarrConfig
metadata:
  name: sonarr
  namespace: media
spec:
  connection:
    url: http://sonarr:8989
    apiKeySecretRef:
      name: sonarr-credentials
      key: api-key
  naming:
    renameEpisodes: true
    standardEpisodeFormat: "{Series TitleYear} - S{season:00}E{episode:00} - {Episode CleanTitle} [{Quality Full}]{-Release Group}"
  rootFolders:
    - path: /media/tv
```

Jellyfin and Seerr get their first-time setup wizard run automatically on a fresh
install.

## Install

Nine independent operators, grouped by what the app does, one chart each.
Install the ones you need:

```bash
helm install media-operator-pvr \
  oci://ghcr.io/kyleseneker/media-operator/media-operator-pvr \
  --namespace media
```

- `pvr` — SonarrConfig, RadarrConfig, LidarrConfig, ReadarrConfig
- `indexers` — ProwlarrConfig, FlareSolverrConfig
- `subtitles` — BazarrConfig
- `downloads` — QBittorrentConfig, SabnzbdConfig
- `mediaservers` — JellyfinConfig, PlexConfig
- `requests` — SeerrConfig
- `curation` — MaintainerrConfig
- `transcode` — TdarrConfig
- `automation` — AutobrrConfig

Each chart ships only its own CRDs and RBAC. Set `watchNamespace` to restrict an
operator to a single namespace; it watches everything by default.

## Configure

Start from [examples/](examples/) — [minimal-sonarr.yaml](examples/minimal-sonarr.yaml)
to check the operator works, [jellyfin-stack.yaml](examples/jellyfin-stack.yaml) or
[plex-stack.yaml](examples/plex-stack.yaml) for a full stack. [config/samples/](config/samples/)
has one file per CRD exercising every field, and the fields themselves are
documented on the CRDs (`kubectl explain sonarrconfig.spec`).

API keys and any other credential come from Secrets. Plain, Sealed, External —
doesn't matter, the operator just reads them and re-reconciles when they change.

## Things worth knowing

- **`prune` won't touch anything you made by hand.** With
  `spec.reconcile.prune: true` the operator only deletes resources it created
  itself, tracked in `status.managedResources`. Root folders and tags are never
  pruned, and prune bails out entirely if more than 25 candidates turn up.
- **Deleting a CR leaves the app's config alone.** `deletionPolicy` defaults to
  `orphan`. For Sonarr, Radarr, Lidarr, Readarr, Prowlarr, and FlareSolverr, set it to
  `delete` to clean up tracked, prunable resources. Root folders, tags, and
  singleton settings remain in place. Other integrations reject `delete` with
  `InvalidConfig`. `delete` also conflicts with `driftPolicy: observe`. Cleanup
  retries for up to 10 minutes; after that the finalizer is released with a warning
  event, potentially leaving remote resources behind.
- **Settings drift is corrected every cycle; root folders and Jellyfin libraries
  are create-only.** Once they exist the operator won't modify them.
- **Prowlarr takes tag labels, the other Servarr apps take tag IDs.** Prowlarr
  resolves labels to IDs at apply time. Everywhere else `tags` is a list of ints.
- **Any `fields` entry can read from a Secret.** Use `valueFrom` instead of
  `value` to keep indexer API keys and webhook URLs out of git.
- **Self-signed certs work.** `connection.tls.caSecretRef` for a private CA, or
  `insecureSkipVerify` if you don't care.

`spec.reconcile.driftPolicy: observe` supports Sonarr, Radarr, Lidarr, Readarr,
Prowlarr, qBittorrent, Jellyfin, and FlareSolverr. It reads configuration without
changing it; qBittorrent and Jellyfin may authenticate to read protected settings.
A fresh Jellyfin is reported as needing initialization without running its wizard.
Create-only resources are compared by existence. Masked credentials cannot be
verified by reading the application.

Other integrations reject `observe` with `InvalidConfig` before making application
requests. They require `enforce` (the default) until read-only comparison is
implemented. Observe-mode differences increment `media_operator_drift_observed_total`,
not `media_operator_drift_corrected_total`. qBittorrent, Jellyfin, and FlareSolverr
report `Synced=False` with reason `DriftDetected` when differences remain.

When upgrading, existing `status.managedResources` records are retained. Older
versions also recorded matching pre-existing resources as managed; the operator
cannot determine their origin retroactively. Review these records before enabling
prune on an existing installation. New Servarr and FlareSolverr ownership records are added only
after successful creation, and failed updates or deletions retain ownership so
cleanup can be retried.

Metrics are on `:8443/metrics` over HTTPS behind TokenReview auth, enabled by
default. `media_operator_config_synced` is the one to alert on — it goes to 0 when
a resource stops reconciling, which no error counter will tell you.

## Development

Needs Go 1.25+ and kubebuilder.

```bash
BINARY=pvr make run          # run one operator against your current kube context
make build                   # all nine binaries
make test
make manifests generate      # after editing api/
make sync-crds               # copy generated CRDs into the charts
```

[CONTRIBUTING.md](CONTRIBUTING.md) has the rest. Apache 2.0, see [LICENSE](LICENSE).

### Seerr quality profiles

Set `activeProfileName` on Seerr's Sonarr/Radarr connections to resolve the current profile ID from that service during each reconciliation. Names take precedence over `activeProfileId`, allowing a restored or recreated profile to receive a new ID. Run Recyclarr (or the profile owner) first. If the name is missing, ambiguous or the service cannot be queried, the operator leaves that Seerr connection unchanged and reports reconciliation failure. ID-only configurations retain their existing behavior.
