package hack

import (
	"encoding/json"
	"os"
	"regexp"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Renovate only updates Makefile tool pins its custom regex manager matches;
// a pin added without a "# renovate:" annotation silently stops updating.
func TestRenovateTracksEveryMakefileToolVersion(t *testing.T) {
	var config struct {
		CustomManagers []struct {
			ManagerFilePatterns []string `json:"managerFilePatterns"`
			MatchStrings        []string `json:"matchStrings"`
		} `json:"customManagers"`
	}
	raw, err := os.ReadFile("../renovate.json")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(raw, &config))

	var manager *regexp.Regexp
	for _, m := range config.CustomManagers {
		if slices.Contains(m.ManagerFilePatterns, "/^Makefile$/") {
			manager = regexp.MustCompile(m.MatchStrings[0])
		}
	}
	require.NotNil(t, manager, "renovate.json has no custom manager for the Makefile")

	makefile, err := os.ReadFile("../Makefile")
	require.NoError(t, err)

	extracted := map[int]bool{} // offsets where Renovate reads a currentValue
	cv := manager.SubexpIndex("currentValue")
	for _, m := range manager.FindAllSubmatchIndex(makefile, -1) {
		extracted[m[2*cv]] = true
	}

	pins := regexp.MustCompile(`(?m)^(\w+_VERSION) \?= (v\S+)$`).FindAllSubmatchIndex(makefile, -1)
	require.NotEmpty(t, pins)
	for _, p := range pins {
		assert.True(t, extracted[p[4]], "%s has no # renovate: annotation", makefile[p[2]:p[3]])
	}
}

// make lint and CI must run the same golangci-lint; Renovate bumps both in one PR.
func TestGolangciLintVersionMatchesCI(t *testing.T) {
	makefile, err := os.ReadFile("../Makefile")
	require.NoError(t, err)
	ci, err := os.ReadFile("../.github/workflows/ci.yml")
	require.NoError(t, err)

	local := regexp.MustCompile(`(?m)^GOLANGCI_LINT_VERSION \?= (\S+)$`).FindSubmatch(makefile)
	require.NotNil(t, local, "GOLANGCI_LINT_VERSION not found in Makefile")
	remote := regexp.MustCompile(`golangci-lint-action@[^\n]*\n\s+with:\s+version: (\S+)`).FindSubmatch(ci)
	require.NotNil(t, remote, "golangci-lint-action version not found in ci.yml")

	assert.Equal(t, string(local[1]), string(remote[1]))
}
