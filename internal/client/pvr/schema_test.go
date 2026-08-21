package pvr

import (
	"strings"
	"testing"
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
	if err := ValidateFieldNames(qbitSchema(), "QBittorrent", []string{"host", "tvCategory"}); err != nil {
		t.Errorf("valid fields rejected: %v", err)
	}
}

// The apps store an unknown field and answer 2xx, so a typo has to be caught here
// or it is silently inert.
func TestValidateFieldNamesRejectsTypos(t *testing.T) {
	err := ValidateFieldNames(qbitSchema(), "QBittorrent", []string{"tvCatagory"})
	if err == nil {
		t.Fatal("a misspelled field was accepted")
	}
	if !strings.Contains(err.Error(), "tvCatagory") {
		t.Errorf("error does not name the offending field: %v", err)
	}
	if !strings.Contains(err.Error(), "tvCategory") {
		t.Errorf("error does not list the accepted names, so it is not actionable: %v", err)
	}
}

func TestValidateFieldNamesIsCaseInsensitiveOnImplementation(t *testing.T) {
	if err := ValidateFieldNames(qbitSchema(), "qbittorrent", []string{"nope"}); err == nil {
		t.Error("implementation lookup should not be case sensitive")
	}
}

// An unknown implementation or unreadable schema must not block a write, since the
// operator cannot tell a bad field from an app it does not have a schema for.
func TestValidateFieldNamesSkipsWhenSchemaUnknown(t *testing.T) {
	if err := ValidateFieldNames(qbitSchema(), "Deluge", []string{"anything"}); err != nil {
		t.Errorf("unknown implementation should skip validation, got: %v", err)
	}
	if err := ValidateFieldNames(nil, "QBittorrent", []string{"anything"}); err != nil {
		t.Errorf("absent schema should skip validation, got: %v", err)
	}
}
