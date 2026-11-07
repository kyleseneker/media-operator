package pvr

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func qbitSchema() []map[string]any {
	return []map[string]any{
		{
			"implementation": "QBittorrent",
			"fields": []any{
				map[string]any{"name": "host"},
				map[string]any{"name": "port"},
				map[string]any{"name": "tvCategory"},
				map[string]any{"name": "tvImportedCategory"},
			},
		},
		{
			"implementation": "Sabnzbd",
			"fields":         []any{map[string]any{"name": "apiKey"}},
		},
	}
}

func TestValidateFieldNamesAcceptsKnownFields(t *testing.T) {
	assert.NoError(t, ValidateFieldNames(qbitSchema(), "QBittorrent", []string{"host", "tvCategory"}), "valid fields rejected")
}

// The apps store an unknown field and answer 2xx, so a typo has to be caught here
// or it is silently inert.
func TestValidateFieldNamesRejectsTypos(t *testing.T) {
	err := ValidateFieldNames(qbitSchema(), "QBittorrent", []string{"tvCatagory"})
	require.Error(t, err, "a misspelled field was accepted")
	assert.ErrorContains(t, err, "tvCatagory", "error does not name the offending field")
	assert.ErrorContains(t, err, "tvCategory", "error does not list the accepted names, so it is not actionable")
}

func TestValidateFieldNamesIsCaseInsensitiveOnImplementation(t *testing.T) {
	assert.Error(t, ValidateFieldNames(qbitSchema(), "qbittorrent", []string{"nope"}),
		"implementation lookup should not be case sensitive")
}

// An unknown implementation or unreadable schema must not block a write, since the
// operator cannot tell a bad field from an app it does not have a schema for.
func TestValidateFieldNamesSkipsWhenSchemaUnknown(t *testing.T) {
	assert.NoError(t, ValidateFieldNames(qbitSchema(), "Deluge", []string{"anything"}), "unknown implementation should skip validation")
	assert.NoError(t, ValidateFieldNames(nil, "QBittorrent", []string{"anything"}), "absent schema should skip validation")
}

func TestSchemaConfigContractComesFromTheAppSchema(t *testing.T) {
	schema := []map[string]any{
		{"implementation": "CustomImport", "configContract": "CustomSettings"},
		{"implementation": "TraktPopularImport", "configContract": "TraktPopularSettings"},
	}
	assert.Equal(t, "CustomSettings", SchemaConfigContract(schema, "customimport", ""),
		"the implementation+Settings guess is not what the app expects")
	assert.Equal(t, "Declared", SchemaConfigContract(schema, "CustomImport", "Declared"), "a declared contract must win")
	assert.Empty(t, SchemaConfigContract(nil, "CustomImport", ""), "an unreadable schema must leave the builder's fallback")
}

func TestSchemaProtocolUsesTheAppsSpelling(t *testing.T) {
	schema := []map[string]any{{"implementation": "QBittorrent", "protocol": "TorrentDownloadProtocol"}}
	assert.Equal(t, "TorrentDownloadProtocol", SchemaProtocol(schema, "qbittorrent", "torrent"),
		"Lidarr reads torrent back as TorrentDownloadProtocol and rewrites it forever")
	assert.Equal(t, "torrent", SchemaProtocol(nil, "QBittorrent", "torrent"), "an unreadable schema must keep the declared protocol")
}

func TestUnsupportedEventsAreTheOnesTheImplementationLacks(t *testing.T) {
	schema := []map[string]any{{"implementation": "Webhook", "onGrab": false, "onReleaseImport": false, "includeHealthWarnings": false}}
	payload := map[string]any{"name": "hook", "onGrab": true, "onDownload": true, "onHealthRestored": true, "includeHealthWarnings": true}
	got := UnsupportedEvents(schema, "Webhook", payload)
	assert.Equal(t, []string{"onDownload", "onHealthRestored"}, got)
	assert.Nil(t, UnsupportedEvents(nil, "Webhook", payload), "an unreadable schema must not reject events")
}
