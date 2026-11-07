package export

import (
	"slices"

	flaresolverrclient "github.com/kyleseneker/media-operator/internal/client/flaresolverr"
)

func exportFlareSolverr(r *reader) (map[string]any, error) {
	names, err := flaresolverrclient.NewClient(r.hc).ListSessions(r.ctx)
	if err != nil {
		return nil, err
	}
	spec := map[string]any{}
	if len(names) == 0 {
		return spec, nil
	}
	sessions := make([]any, 0, len(names))
	for _, name := range slices.Sorted(slices.Values(names)) {
		sessions = append(sessions, map[string]any{"name": name})
	}
	spec["sessions"] = sessions
	return spec, nil
}
