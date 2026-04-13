# Contributing to media-operator

Thanks for your interest in contributing to media-operator.

## Getting Started

1. Fork the repository
2. Clone your fork: `git clone https://github.com/<your-username>/media-operator`
3. Create a branch: `git checkout -b my-feature`
4. Make your changes
5. Run tests: `make test`
6. Push and open a PR

## Development Setup

- Go 1.22+
- kubebuilder
- A Kubernetes cluster (kind works fine for development)
- Access to the app(s) you're working on (Sonarr, Radarr, etc.)

```bash
# Generate deepcopy and CRD manifests after changing types
make generate manifests

# Run a specific operator locally against your cluster
BINARY=servarr make run       # servarr, downloads, mediaservers, requests, transcode

# Build all binaries
make build

# Run tests
make test
```

## Project Structure

media-operator is split into 5 domain-specific operators:

| Binary | Apps | API Package |
|--------|------|-------------|
| `media-operator-servarr` | Sonarr, Radarr, Lidarr, Readarr, Prowlarr, Bazarr | `api/servarr/v1alpha1/` |
| `media-operator-downloads` | qBittorrent, SABnzbd | `api/downloads/v1alpha1/` |
| `media-operator-mediaservers` | Jellyfin, Plex | `api/mediaservers/v1alpha1/` |
| `media-operator-requests` | Seerr/Jellyseerr | `api/requests/v1alpha1/` |
| `media-operator-transcode` | Tdarr | `api/transcode/v1alpha1/` |

Shared types live in `api/common/v1alpha1/`. Shared controller helpers live in `internal/controller/common/`.

## Adding a New App

To add support for a new app:

1. Determine which domain group it belongs to (or create a new one)
2. Define the types in the appropriate `api/{domain}/v1alpha1/yourappconfig_types.go`
3. Create an API client in `internal/client/yourapp/client.go`
4. Implement the controller in `internal/controller/{domain}/yourappconfig_controller.go`
5. Register the controller in `cmd/{domain}/main.go`
6. Add a sample CR in `config/samples/`
7. Run `make generate manifests` to regenerate CRDs
8. Write tests

## Code Style

- Follow standard Go conventions
- Run `make lint` before submitting
- Keep controllers focused — one CR type per controller
- Use the shared `reconciler` package for common operations
- All Secret values must use `SecretKeyRef` — never put credentials in CR specs

## Reporting Issues

Open an issue on GitHub. Include:
- The CRD and CR you applied (redact secrets)
- The operator logs (`kubectl logs -n media deployment/media-operator-servarr`)
- The app version you're targeting
- What you expected vs what happened
