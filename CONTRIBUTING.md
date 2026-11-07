# Contributing to media-operator

## Getting started

1. Fork and clone: `git clone https://github.com/<your-username>/media-operator`
2. Branch: `git checkout -b my-feature`
3. Make your changes
4. `make test lint verify-crds`
5. Push and open a PR

You'll need the Go version in `go.mod`, kubebuilder, a cluster (kind is fine),
and access to whichever app you're working on. Most of the interesting behavior
only shows up against a real Sonarr or Jellyfin, so it helps to have one
running.

```bash
BINARY=pvr make run          # run one operator against your current kube context
make build                   # every operator and the CLI
BINARY=pvr make docker-build # that operator's image for this machine; IMG= to tag, ARCH= to cross-build
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

## Dependency updates

Renovate (`renovate.json`) opens PRs on Saturday mornings; none automerge. The
Kubernetes libraries and controller-runtime arrive as one PR. A controller-tools
PR needs `make manifests generate sync-crds` run and committed on its branch
before `verify-crds` passes.

## Commit messages and releases

Commits use [Conventional Commits](https://www.conventionalcommits.org/) with
the app as the scope: `fix(tdarr): ...`, `feat(prowlarr): ...`. Release notes
are generated from them, so `feat` and `fix` subjects should read as a change a
user would notice. `chore(deps)` commits are listed as dependencies. `test`,
`docs`, `refactor`, `style`, `ci` and other `chore` commits are left out. Mark
anything that needs action on upgrade with `!` (`feat(maintainerr)!: ...`) and
it is listed under Breaking changes.

A release is an annotated tag on `main`:

```bash
git tag -a v0.37.0 -F- <<'EOF'
v0.37.0: one-line summary, used as the release title

Optional upgrade notes: what a user has to do or check before upgrading.
EOF
git push origin v0.37.0
```

Anything after the subject's blank line goes under Upgrade notes. Git strips
lines starting with `#` from tag messages, so use plain paragraphs or `-`
bullets, not headings. The notes also list `kubectl apply` commands for any CRD
that changed since the previous tag, because Helm never upgrades CRDs.
