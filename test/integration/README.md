# Disposable application API checks

These opt-in tests require isolated Sonarr, Radarr and Prowlarr instances. They
create configuration and delete only their uniquely named test resources. Never
point them at production. Provision applications yourself with new config volumes,
record the image versions, and initialize authentication using each image's
supported mechanism. No particular image vendor or deployment tool is required.

```sh
export MEDIA_OPERATOR_TEST_DISPOSABLE=1
export MEDIA_TEST_SONARR_URL=http://localhost:8989
export MEDIA_TEST_RADARR_URL=http://localhost:7878
export MEDIA_TEST_PROWLARR_URL=http://localhost:9696
# Supply keys from the disposable instances (avoid committing them):
export MEDIA_TEST_SONARR_API_KEY=...
export MEDIA_TEST_RADARR_API_KEY=...
export MEDIA_TEST_PROWLARR_API_KEY=...
# Existing writable directories INSIDE each disposable app:
export MEDIA_TEST_SONARR_LOCAL_PATH=/data/downloads
export MEDIA_TEST_RADARR_LOCAL_PATH=/data/downloads
make test-integration
```

The tests log detected versions, round-trip remote mappings through both Servarr
APIs, check that the next pass keeps the mapping's ID, and recreate a Prowlarr
profile to verify name resolution follows its new ID. Prowlarr indexer payloads
are resolved but not submitted: a working external indexer is not required.
Use Linux/POSIX local paths for this live test; Windows path normalization is
covered by the regular unit suite.

The default test suite does not contact these applications. Missing variables in
an explicitly requested integration run fail rather than silently skipping checks.
These checks do not prove full-stack recovery, credential bootstrap, download
imports or compatibility for integrations they do not exercise. Record successful
runs and image versions before advertising a release as tested.
