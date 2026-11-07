# User guide

- [Reconciling](#reconciling)
- [Status](#status)
- [Observe mode](#observe-mode)
- [Prune](#prune)
- [Deletion](#deletion)
- [Credentials](#credentials)
- [References by name](#references-by-name)
- [Metrics](#metrics)
- [Upgrading](#upgrading)
- [Verifying a release](#verifying-a-release)

## Reconciling

The operator reads each app's configuration through its API, compares it with
the resource, and writes what differs. Fields left out of the resource are not
managed. It reconciles every 5 minutes, when the resource changes, and when a
Secret the resource refers to changes. `spec.reconcile.interval` changes the
period.

Download clients, indexers, notifications and other lists are matched by name.
Root folders and Jellyfin and Plex libraries are created if missing and
otherwise left alone.

On a fresh install, the operator runs the Jellyfin and Seerr setup wizards.
Seerr's needs Seerr 3.5.0 or later, and when set up through Jellyfin it waits
until Jellyfin has a library.

## Status

Each resource has a `Ready` condition, for whether the app and its credentials
are reachable, and a `Synced` condition, for whether the app matches. `kubectl
get` shows both. The `Synced` reason is one of:

| Reason | Meaning |
| --- | --- |
| `Synced` | The app matches the resource. |
| `SyncFailed` | Some settings could not be applied; the message says which. |
| `AppUnreachable` | The app did not respond, or rejected the credentials. |
| `SecretNotFound` | A referenced Secret or key does not exist. |
| `InvalidConfig` | The resource asks for something the operator does not support. |
| `Observed` | Observe mode found no differences. |
| `DriftDetected` | Observe mode found differences. |

## Observe mode

`spec.reconcile.driftPolicy: observe` compares without writing to the app.
Differences set `Synced` to `False` with reason `DriftDetected` and increment
`media_operator_drift_observed_total`.

Some things can't be compared by reading:

- qBittorrent, Jellyfin and Seerr still sign in to read protected settings.
- An uninitialized Jellyfin or Seerr is reported as needing setup; the wizard
  doesn't run.
- A new Maintainerr reports rule groups as different until the media server and
  the Sonarr and Radarr servers they use exist.
- Root folders and Jellyfin and Plex libraries are compared by existence.
- Passwords and API keys that an app masks count as different only if the
  operator wrote them and the Secret has changed since.
- Plex preferences that Plex doesn't list are not compared.

## Prune

With `spec.reconcile.prune: true`, the operator deletes items it created once
they are removed from the resource. It tracks what it created in
`status.managedResources`; items it didn't create are never deleted. Without
prune, removing an item from the resource only stops managing it.

- A list with no declared items is not pruned.
- Root folders, tags and Prowlarr sync profiles are never pruned.
- A pass that would delete more than 25 items deletes none.
- Observe mode never prunes.

Supported for Sonarr, Radarr, Lidarr, Readarr, Prowlarr, FlareSolverr and Tdarr
library variables. Other resources reject `prune: true` with `InvalidConfig`.

## Deletion

Deleting a resource leaves the app unchanged. With
`spec.reconcile.deletionPolicy: delete`, the operator first deletes the items it
created, following the prune rules. Settings, root folders and tags are kept.

`delete` is supported for Sonarr, Radarr, Lidarr, Readarr, Prowlarr and
FlareSolverr, and not in observe mode; anything else is rejected with
`InvalidConfig`. If cleanup keeps failing, the operator gives up after 10
minutes, records a warning event and removes the finalizer.

## Credentials

Credentials come from Secrets, referenced by name and key. Any kind of Secret
works, including Sealed and External Secrets.

The `fields` of Sonarr, Radarr, Lidarr and Readarr indexers, notifications and
import lists, and of Prowlarr indexers, download clients and notifications, can
read a value from a Secret with `valueFrom` instead of `value`. Bazarr provider
settings use the value `ENV:<secret>:<key>`.

For a private certificate authority, set `connection.tls.caSecretRef`.
`connection.tls.insecureSkipVerify` disables verification.

## References by name

The apps link resources by ID, and IDs change when an app is rebuilt. These
references can use names instead:

- `tagLabels` on Sonarr, Radarr, Lidarr and Readarr download clients, indexers,
  notifications and import lists. Tags in `spec.tags` are created first.
- Custom formats in quality profiles, and quality profiles in import lists.
- `appProfileName` on Prowlarr indexers.
- `activeProfileName` on Seerr's Sonarr and Radarr connections, looked up from
  that Sonarr or Radarr.

A name that matches nothing, or more than one item, fails the reconcile. See
[Rebuilding apps](rebuilding.md) for more.

## Metrics

Metrics are served on `:8443/metrics` over HTTPS and require a token with access
to them. Set `metrics.serviceMonitor.enabled` in the chart to create a
ServiceMonitor.

| Metric | Description |
| --- | --- |
| `media_operator_config_synced` | 1 if the resource last reconciled successfully, otherwise 0 |
| `media_operator_drift_corrected_total` | Items rewritten because the app differed |
| `media_operator_drift_observed_total` | Differences found in observe mode |
| `media_operator_resources_pruned_total` | Items deleted by prune |
| `media_operator_managed_resources` | Items declared per resource type |
| `media_operator_app_api_request_duration_seconds` | Request latency to each app |
| `media_operator_app_api_errors_total` | Failed requests to each app |

`media_operator_config_synced == 0` is the condition to alert on.

## Upgrading

Helm installs CRDs on first install only. When a release changes CRDs, its
release notes include the `kubectl apply` commands to update them. Breaking
changes are listed at the top of the release notes.

Versions before 0.34.2 also recorded pre-existing items in
`status.managedResources`. Review that list before enabling prune on a resource
created by one of those versions.

## Verifying a release

From v0.37.0, images and charts are signed with [cosign](https://docs.sigstore.dev/)
by the release workflow:

```bash
cosign verify ghcr.io/kyleseneker/media-operator-pvr:0.37.0 \
  --certificate-identity https://github.com/kyleseneker/media-operator/.github/workflows/release.yml@refs/tags/v0.37.0 \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

Charts verify the same way, as
`ghcr.io/kyleseneker/media-operator/media-operator-pvr:0.37.0`. Images include an
SBOM:

```bash
docker buildx imagetools inspect ghcr.io/kyleseneker/media-operator-pvr:0.37.0 --format '{{ json .SBOM }}'
```
