# Security

## Reporting a vulnerability

Use [private vulnerability reporting](https://github.com/kyleseneker/media-operator/security/advisories/new)
rather than opening a public issue.

This is a side project with one maintainer, so I'm not going to pretend to a
response SLA. I'll acknowledge what I can, as fast as I can.

## Supported versions

The latest release only. There are no backport branches.

## What this operator has access to

Worth understanding before you deploy it:

- **It holds every API key in your media stack.** Sonarr, Radarr, Plex tokens,
  download client passwords — anything you reference from a CR gets read out of a
  Secret and sent to the corresponding app. Its ServiceAccount can read Secrets in
  whatever namespaces it watches. Set `watchNamespace` to narrow that down.
- **It can delete things in those apps.** `prune` and
  `deletionPolicy: delete` are both opt-in and both scoped to resources the
  operator created, but they are real delete calls.
- **`insecureSkipVerify` does what it says.** It disables certificate
  verification for that app's connection. Fine on a LAN you trust, not fine over
  anything you don't. Prefer `caSecretRef` with your own CA.
- **Metrics are authenticated.** The `:8443/metrics` endpoint requires a
  TokenReview-validated bearer token, served over HTTPS with a self-signed cert.
  Resource names appear in `media_operator_config_synced` labels; no credentials
  do.

Credentials are never written to CR specs or status. If you find one leaking into
a log line or a status message, that's a bug worth reporting privately.
