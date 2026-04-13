package engine

// ReconcilePolicy controls how a resource endpoint is reconciled.
type ReconcilePolicy int

const (
	// UpdateAlways GETs the singleton config, merges desired over current, PUTs back.
	UpdateAlways ReconcilePolicy = iota
	// CreateOnly creates the resource if missing, never updates.
	CreateOnly
	// CreateOrUpdate creates if missing, updates if exists (matched by MatchField).
	CreateOrUpdate
)

// AuthType identifies the authentication strategy for an app.
type AuthType int

const (
	// AuthNone applies no authentication headers (FlareSolverr, SABnzbd with key in body).
	AuthNone AuthType = iota
	// AuthAPIKey uses X-Api-Key header (Servarr family, Tdarr, Autobrr, Maintainerr).
	AuthAPIKey
	// AuthCookie uses session-based cookie auth (qBittorrent).
	AuthCookie
	// AuthMediaBrowser uses Jellyfin's MediaBrowser auth header.
	AuthMediaBrowser
	// AuthPlexToken uses X-Plex-Token header.
	AuthPlexToken
	// AuthFormEncoded uses X-API-KEY header with form-encoded bodies (Bazarr).
	AuthFormEncoded
	// AuthSession uses cookie jar with optional X-Api-Key fallback (Seerr).
	AuthSession
)

// SettingEndpoint defines a singleton config endpoint (GET id, merge, PUT).
type SettingEndpoint struct {
	// Name identifies this setting for status reporting.
	Name string
	// Path is the API path (e.g., "/api/v3/config/mediamanagement").
	Path string
}

// ResourceEndpoint defines a list-based resource endpoint.
type ResourceEndpoint struct {
	// Name identifies this resource type for status reporting.
	Name string
	// Path is the API path (e.g., "/api/v3/rootfolder").
	Path string
	// MatchField is the field used to match existing resources (e.g., "path", "name").
	MatchField string
	// Policy controls create/update behavior.
	Policy ReconcilePolicy
	// Prunable indicates this resource type supports deletion of unmanaged items.
	// Root folders and tags should not be prunable.
	Prunable bool
}

// AppDefinition describes how to reconcile an app's configuration.
type AppDefinition struct {
	// Settings are singleton config endpoints.
	Settings []SettingEndpoint
	// Resources are list-based resource endpoints.
	Resources []ResourceEndpoint
	// HealthPath is the endpoint to check if the app is reachable.
	HealthPath string
}
