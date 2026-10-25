# Rebuilding application configuration

The operator configures running applications. Deploy their containers, storage,
networking and credentials separately. A successful reconcile does not restore
media, watch history, requests, download queues or other application databases.

## Portable references

Use [portable-references.yaml](../examples/portable-references.yaml) as a starting
point; replace its endpoints, paths and Secret references before applying it.

- Sonarr, Radarr, Lidarr and Readarr download clients, indexers, notifications and
  import lists accept `tagLabels`. Nonempty labels take precedence over legacy
  numeric `tags`. Declare tags in `spec.tags` or reference labels already present
  in the app. Tags declared in the same CR are created before reference lookup.
- Prowlarr's existing string `tags` fields remain labels. Notifications use the
  shared `tagLabels` field. Unresolved labels now fail reconciliation instead of
  silently disappearing from the API payload.
- Prowlarr `appProfiles` creates/updates sync profiles before indexers.
  `indexers[].appProfileName` resolves the current profile ID and takes precedence
  over `appProfileId`. Existing ID-only manifests still work. Profiles are retained
  during prune and CR deletion because unmanaged indexers may reference them.
- Quality profiles resolve custom-format names after formats are applied; import
  lists resolve quality-profile names after profiles are applied. Missing or
  ambiguous names block the dependent stage instead of producing partial payloads.
- Seerr already supports `activeProfileName`. Run the profile owner (for example,
  Recyclarr) before expecting those connections to reconcile successfully.

Resolution runs every cycle, so recreated resources can receive new IDs. Missing,
ambiguous or unreadable references report failure and stop the dependent stage;
prerequisites already created remain recorded in status. This is not a transaction:
other stages may already have made changes. Observe mode never creates prerequisites
and reports unresolved references if they do not yet exist.

## Remote download paths

Sonarr, Radarr, Lidarr and Readarr support `remotePathMappings` with `host`,
`remotePath` and `localPath`. The host must match the configured download-client
hostname. The local directory must already exist and be accessible **inside the
app's container**. The disposable-app checks exercise Sonarr and Radarr; Lidarr and
Readarr share the same endpoint and are covered by the regular Go suite.

Identity is `(host, remotePath)`, not just the directory. Changing `localPath`
updates a mapping in place. Directory paths receive trailing separators to match
Servarr's storage format, including Windows paths. Changing host or remotePath
creates a new identity; use prune with a nonempty desired mapping list to remove
old operator-created mappings. Omission or an empty list leaves existing mappings
alone, consistent with other resources. Manually created mappings are never adopted
for deletion. `deletionPolicy: delete` cleans up tracked mappings.

## Rebuild acceptance procedure

Use isolated applications with disposable configuration volumes and test media
paths. Do not run destructive rebuild tests against a production database.

1. Record each app image/version. Start with empty configuration volumes. Deploy
   the CRDs/operator and create the Secrets the applications and operator will use.
2. Establish authentication. Either configure a known API key through the app's
   supported deployment mechanism or import its generated key into a Secret.
   Key discovery/adoption is not automatic. Jellyfin/Seerr support setup workflows,
   but that does not bootstrap credentials for every application.
3. Apply configuration with a single owner for each setting. For example, let
   Recyclarr own quality profiles/custom formats/naming and Prowlarr own downstream
   indexers; omit those sections from competing CRs.
4. Verify CR status and read back the actual settings through each application's
   API/UI. Confirm associations use the freshly assigned IDs. A second reconciliation
   should converge without repeated configuration writes.
5. Recreate the disposable apps, arrange for different generated IDs, and reapply
   the same manifests. Repeat the checks without editing IDs in Git.
6. Exercise one missing reference and one unavailable dependency. Confirm failure
   is visible, existing associations stay intact, and retry succeeds after fixing
   the dependency.

The regular Go suite covers these ordering/failure cases with stateful HTTP
fixtures. `make test-envtest` checks Kubernetes schema acceptance. Neither proves
compatibility with a particular application release. See
[the disposable-app checks](../test/integration/README.md) for live API validation.

## Remaining rebuild limitations

Bazarr profiles currently require explicit `profileId` values; those are declared
profile identities, not a name-to-generated-ID lookup. Validate defaults and any
existing media assignments after a rebuild. Jellyfin libraries remain create-only.
Database-backed users, histories, media assignments and settings outside the CRDs
still require backups or separate provisioning. This milestone does not establish
fully unattended recovery of an entire media stack.
