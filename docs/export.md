# Exporting an existing app

`media-operator export` reads a running app and prints a config resource that
matches it, plus a Secret for its credentials.

## Install

Download the `media-operator-cli` archive for your platform from the
[releases page](https://github.com/kyleseneker/media-operator/releases), or run
the `ghcr.io/kyleseneker/media-operator-cli` image. Both are signed; see
[Verifying a release](guide.md#verifying-a-release).

## Usage

```bash
media-operator export <app> --url <url> [flags] > config.yaml
```

From a workstation, through a port-forward:

```bash
kubectl -n media port-forward svc/sonarr 8989 &
media-operator export sonarr \
  --url http://localhost:8989 --connection-url http://sonarr.media:8989 \
  --api-key "$SONARR_API_KEY" --namespace media > sonarr.yaml
```

From inside the cluster:

```bash
kubectl -n media run export --rm -i --quiet --restart=Never \
  --image ghcr.io/kyleseneker/media-operator-cli:<version> \
  -- export sonarr --url http://sonarr:8989 --api-key "$SONARR_API_KEY" --namespace media > sonarr.yaml
```

| Flag | Description |
| --- | --- |
| `--url` | URL to read the app from |
| `--connection-url` | URL the operator should use, if different from `--url` |
| `--api-key` | API key, or `$MEDIA_OPERATOR_API_KEY` |
| `--username`, `--password` | Login for qBittorrent and Jellyfin, or `$MEDIA_OPERATOR_USERNAME` and `$MEDIA_OPERATOR_PASSWORD` |
| `--token` | Plex token, or `$MEDIA_OPERATOR_TOKEN` |
| `--name` | Name of the resource; defaults to the app |
| `--namespace` | Namespace of the resource and Secret |
| `--secret` | Name of the Secret; defaults to `<name>-credentials` |
| `--exclude` | Comma-separated spec fields to leave out |
| `--insecure-skip-verify` | Skip TLS certificate verification |

FlareSolverr and Maintainerr need no credentials. Every other app takes
`--api-key`, except as listed above.

## Output

Credentials the app returns are written to the Secret. Ones it masks are left
empty and listed on stderr; fill them in before applying.

Fields still at the app's defaults are omitted where the app reports its
defaults. The command also prints a warning for anything the resource can't
represent or the app doesn't report, such as a second Sonarr server in Seerr or
a Plex preference Plex doesn't list.

Use `--exclude` for parts managed by another tool, for example `indexers` when
Prowlarr syncs them, or `customFormats,qualityProfiles` when Recyclarr does.

## Applying the result

Set `spec.reconcile.driftPolicy: observe` before the first apply. The resource
should report `Synced` with reason `Observed`. If it reports `DriftDetected`
instead, compare the export with the app before removing observe mode.
