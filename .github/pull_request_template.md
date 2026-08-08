## What this changes

<!-- Brief description. Link the issue if there is one. -->

## Testing

<!--
Which app and version did you test against? Unit tests are good but these
integrations mostly break against real APIs, so say what you actually ran this
against if you could.
-->

## Checklist

- [ ] `make test lint` passes
- [ ] Ran `make manifests generate sync-crds` if I touched `api/`
- [ ] Added or updated a sample in `config/samples/` for new fields
- [ ] New CRD fields have comments explaining them to someone unfamiliar with the app's API
- [ ] If I added a new domain group, it's wired into `.goreleaser.yml`, the release workflow's chart matrix, and `BINARIES`
