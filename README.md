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
install. Seerr's setup needs Seerr 3.5.0 or later; with Jellyfin it waits until
Jellyfin has at least one library, then enables every library.

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

### Verifying a release

From v0.37.0 on, the release workflow signs every image and chart with
[cosign](https://docs.sigstore.dev/), keylessly, so the signature proves it was
built by this repository's release workflow for that tag:

```bash
cosign verify ghcr.io/kyleseneker/media-operator-pvr:0.37.0 \
  --certificate-identity https://github.com/kyleseneker/media-operator/.github/workflows/release.yml@refs/tags/v0.37.0 \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Charts verify the same way, as
`ghcr.io/kyleseneker/media-operator/media-operator-pvr:0.37.0`. Each image also
carries an SBOM:
`docker buildx imagetools inspect ghcr.io/kyleseneker/media-operator-pvr:0.37.0 --format '{{ json .SBOM }}'`.

## Compatibility

CI runs every operator against the real application in a container
([test/compat](test/compat/)). The first reconcile has to reach `Synced` and a
second one has to send no writes. For apps that support `driftPolicy: observe`,
observe mode has to send none either. These are the versions it runs; Renovate
moves them forward. Older versions may work but aren't tested.

| App | Tested version |
| --- | --- |
| Sonarr | 4.0.20 |
| Radarr | 6.4.4 |
| Lidarr | 3.1.6 |
| Readarr | 0.4.18 (develop) |
| Prowlarr | 2.7.0 |
| FlareSolverr | 3.5.2 |
| Bazarr | 1.6.2 |
| qBittorrent | 5.3.0 |
| SABnzbd | 5.1.3 |
| Jellyfin | 12.4 |
| Plex | 1.43.5, unclaimed |
| Seerr | 3.5.1, set up through Jellyfin |
| Maintainerr | 3.32.0, with Jellyfin |
| Tdarr | 3.06.00, auth off |
| Autobrr | 1.90.0 |

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
  pruned, and prune bails out entirely if more than 25 candidates turn up. It
  covers Sonarr, Radarr, Lidarr, Readarr, Prowlarr and FlareSolverr resources and
  Tdarr library variables. The other operators reject `prune` with
  `InvalidConfig` rather than ignore it. Without prune, nothing removed from a
  CR is deleted.
- **Deleting a CR leaves the app's config alone.** `deletionPolicy` defaults to
  `orphan`. For Sonarr, Radarr, Lidarr, Readarr, Prowlarr, and FlareSolverr, set it to
  `delete` to clean up tracked, prunable resources. Root folders, tags, and
  singleton settings remain in place. Other integrations reject `delete` with
  `InvalidConfig`. `delete` also conflicts with `driftPolicy: observe`. Cleanup
  retries for up to 10 minutes; after that the finalizer is released with a warning
  event, potentially leaving remote resources behind.
- **Settings drift is corrected every cycle; root folders and Jellyfin libraries
  are create-only.** Once they exist the operator won't modify them.
- **Portable tag references.** Sonarr, Radarr, Lidarr and Readarr resources
  accept `tagLabels`, resolved after declared tags are created. Nonempty labels
  take precedence over numeric `tags`.
  Prowlarr's existing `tags` fields remain labels; its notifications use `tagLabels`.
  Missing or ambiguous references fail instead of silently dropping associations.
- **Prowlarr sync profiles.** Declare `appProfiles` and reference them with
  `indexers[].appProfileName`; names take precedence over `appProfileId`. Profiles
  are created/updated but retained during prune and deletion.
- **Remote downloaders.** Sonarr, Radarr, Lidarr and Readarr
  `remotePathMappings` translate download paths by host and remote directory;
  local paths can be updated in place.
- **Any `fields` entry can read from a Secret.** Use `valueFrom` instead of
  `value` to keep indexer API keys and webhook URLs out of git.
- **Self-signed certs work.** `connection.tls.caSecretRef` for a private CA, or
  `insecureSkipVerify` if you don't care.

Every operator supports `spec.reconcile.driftPolicy: observe`. It reads the app's
configuration, compares it with the CR and changes nothing. Differences set
`Synced=False` with reason `DriftDetected` and increment
`media_operator_drift_observed_total` rather than
`media_operator_drift_corrected_total`; a match sets reason `Observed`. Some
things can't be compared by reading:

- qBittorrent, Jellyfin and Seerr sign in to read protected settings, which
  changes nothing.
- A fresh Jellyfin or Seerr is reported as needing setup; observe doesn't run it.
  A fresh Maintainerr reports rule groups as differing until the media server
  and the Sonarr and Radarr servers they use exist.
- Create-only resources (root folders, Jellyfin and Plex libraries) are compared
  by existence.
- Masked credentials can't be read back, so one counts as differing only after
  the operator has written it and its Secret has changed since. Plex preferences
  that Plex never lists aren't compared at all.

When upgrading, existing `status.managedResources` records are retained. Older
versions also recorded matching pre-existing resources as managed; the operator
cannot determine their origin retroactively. Review these records before enabling
prune on an existing installation. New ownership records for Sonarr, Radarr,
Lidarr, Readarr, Prowlarr, FlareSolverr and Tdarr library variables are added
only after successful creation, and failed updates or deletions retain ownership
so cleanup can be retried.

Metrics are on `:8443/metrics` over HTTPS behind TokenReview auth, enabled by
default. `media_operator_config_synced` is the one to alert on — it goes to 0 when
a resource stops reconciling, which no error counter will tell you.

## Development

Needs Go 1.27+ and kubebuilder.

```bash
BINARY=pvr make run          # run one operator against your current kube context
make build                   # all nine binaries
BINARY=pvr make docker-build # that operator's image for this machine (IMG= to tag, ARCH= to cross-build)
make test
make manifests generate      # after editing api/
make sync-crds               # copy generated CRDs into the charts
```

[CONTRIBUTING.md](CONTRIBUTING.md) has the rest. Apache 2.0, see [LICENSE](LICENSE).

### Seerr quality profiles

Set `activeProfileName` on Seerr's Sonarr/Radarr connections to resolve the current profile ID from that service during each reconciliation. Names take precedence over `activeProfileId`, allowing a restored or recreated profile to receive a new ID. Run Recyclarr (or the profile owner) first. If the name is missing, ambiguous or the service cannot be queried, the operator leaves that Seerr connection unchanged and reports reconciliation failure. ID-only configurations retain their existing behavior.

### Rebuilds and portability

See [the rebuild guide](docs/rebuilding.md) for credential prerequisites, ownership,
remaining limitations and acceptance checks, and
[portable-references.yaml](examples/portable-references.yaml) for a complete example.
Readarr support is legacy: the [upstream project is retired](https://readarr.com/).
