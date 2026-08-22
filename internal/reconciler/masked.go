package reconciler

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"sync"
)

// maskedMinLen is the shortest run of asterisks treated as a mask. Real secrets
// are never all-asterisk, and the apps pad to at least this width.
const maskedMinLen = 4

// IsMasked reports whether a value fetched from an app is the placeholder these
// APIs substitute for secret fields on read. Sonarr, Radarr, Prowlarr and the
// rest return "********" for API keys and passwords, so the value the operator
// wrote back can never be read again to compare against.
func IsMasked(v any) bool {
	s, ok := v.(string)
	if !ok || len(s) < maskedMinLen {
		return false
	}
	return strings.Trim(s, "*") == ""
}

// secretDigest holds the hashes of the secret values the operator last wrote,
// keyed by resource. A masked field cannot be compared against the app, so the
// only way to notice a rotated secret is to remember what was last sent.
//
// The store is process-scoped: after a restart the first pass rewrites each
// secret once and then goes quiet. That is a bounded, converging cost, unlike
// persisting a secret-derived hash into the status of all fourteen CRDs.
var secretDigest = struct {
	sync.Mutex
	hashes map[string]string
}{hashes: map[string]string{}}

// SecretsChangedSince reports whether the digest differs from the one last
// written for key. An empty digest means the resource has no masked fields, so
// there is nothing to remember and nothing to force.
func SecretsChangedSince(key, digest string) bool {
	if digest == "" {
		return false
	}
	secretDigest.Lock()
	defer secretDigest.Unlock()
	prev, seen := secretDigest.hashes[key]
	return !seen || prev != digest
}

// RecordSecrets stores the digest written for key.
func RecordSecrets(key, digest string) {
	if digest == "" {
		return
	}
	secretDigest.Lock()
	defer secretDigest.Unlock()
	secretDigest.hashes[key] = digest
}

// digestOf hashes the desired values that landed on masked fields. Values are
// sorted so the digest does not depend on map iteration order.
func digestOf(values []string) string {
	if len(values) == 0 {
		return ""
	}
	sorted := append([]string(nil), values...)
	sort.Strings(sorted)
	h := sha256.New()
	for _, v := range sorted {
		h.Write([]byte(v))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}
