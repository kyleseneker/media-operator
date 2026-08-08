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
BINARY=servarr make run      # run one operator against your current kube context
make build                   # all seven binaries
make test
make manifests generate      # after editing anything in api/
make sync-crds               # copy generated CRDs into the charts
make lint
```

`BINARY` is one of: `servarr`, `downloads`, `mediaservers`, `requests`,
`transcode`, `automation`, `utilities`.

## Layout

Seven operators, one per domain group. Each has a `cmd/` entrypoint, an API
package, a controller package, and a Helm chart, all named the same way.

| Binary | Apps | API package |
|--------|------|-------------|
| `media-operator-servarr` | Sonarr, Radarr, Lidarr, Readarr, Prowlarr, Bazarr | `api/servarr/v1alpha1/` |
| `media-operator-downloads` | qBittorrent, SABnzbd | `api/downloads/v1alpha1/` |
| `media-operator-mediaservers` | Jellyfin, Plex | `api/mediaservers/v1alpha1/` |
| `media-operator-requests` | Seerr/Jellyseerr, Maintainerr | `api/requests/v1alpha1/` |
| `media-operator-transcode` | Tdarr | `api/transcode/v1alpha1/` |
| `media-operator-automation` | Autobrr | `api/automation/v1alpha1/` |
| `media-operator-utilities` | FlareSolverr | `api/utilities/v1alpha1/` |

Shared types are in `api/common/v1alpha1/`, shared controller helpers in
`internal/controller/common/`. The generic reconcile engine — the thing that
diffs desired against actual and decides create vs update vs prune — is
`internal/engine/`. Most Servarr-shaped apps just declare their endpoints and let
the engine do the work; see `internal/client/servarr/client.go` for what that
looks like.

## Adding a new app

1. Pick the domain group it belongs to, or add one
2. Define types in `api/{domain}/v1alpha1/yourappconfig_types.go`
3. Write the API client in `internal/client/yourapp/client.go`
4. Implement the controller in `internal/controller/{domain}/yourappconfig_controller.go`
5. Register it in `cmd/{domain}/main.go`
6. Add a sample CR to `config/samples/` covering every field
7. `make manifests generate sync-crds`
8. Write tests

If you added a **new domain group**, it also needs releasing, which is easy to
forget and invisible until someone tries to install it:

- a chart under `chart/media-operator-{domain}/`
- a `builds` entry, two `dockers` entries, and two `docker_manifests` entries in `.goreleaser.yml`
- the chart name in the `helm` matrix in `.github/workflows/release.yml`
- the domain in `BINARIES` in the `Makefile`

## Conventions

- Standard Go style; `make lint` must pass
- One CR type per controller
- Credentials only ever arrive via `SecretKeyRef` — never a plain string in a spec
- Anything settable on a resource should be settable from the CR; don't rely on
  the app's UI defaults
- CRD field comments are the user-facing docs, so write them for someone who has
  never seen the app's API

CI runs `make test`, `make lint`, `make build`, and `make verify-crds`. That last
one fails if the CRDs in `chart/*/crds/` don't match what `make manifests`
generates, so run `make sync-crds` after touching `api/`.

## Reporting bugs

Include the CR you applied with secrets redacted, the operator logs
(`kubectl logs -n media deployment/media-operator-servarr`), the app and version
you're pointing at, and what you expected instead. The app's own version matters
more than you'd think — these APIs change between releases and often without
documentation.
