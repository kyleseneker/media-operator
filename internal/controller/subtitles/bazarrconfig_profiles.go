package subtitles

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strconv"

	subtitlesv1alpha1 "github.com/kyleseneker/media-operator/api/subtitles/v1alpha1"
	"github.com/kyleseneker/media-operator/internal/engine"
)

const (
	profilesField = "languages-profiles"
	profilesKind  = "languageProfiles"
)

// bazarrProfiles is the language profile list to save. Bazarr deletes every
// stored profile a save leaves out, so the list carries the profiles the spec
// doesn't declare and leaves out only those prune deletes.
type bazarrProfiles struct {
	existing []map[string]any
	declared []string
	present  []string
	prune    bool
	send     string
}

func planBazarrProfiles(declaredJSON string, existing []map[string]any, prune bool, owned *engine.Ownership) (*bazarrProfiles, error) {
	var declared []map[string]any
	if err := json.Unmarshal([]byte(declaredJSON), &declared); err != nil {
		return nil, fmt.Errorf("reading declared language profiles: %w", err)
	}
	p := &bazarrProfiles{existing: existing, prune: prune}
	for _, d := range declared {
		p.declared = append(p.declared, profileKey(d))
	}
	for _, e := range existing {
		p.present = append(p.present, profileKey(e))
	}
	var drop []string
	if prune {
		drop = owned.Candidates(profilesKind, p.declared, p.present)
		if len(drop) > engine.DefaultMaxPruneCount {
			return nil, fmt.Errorf("refusing to delete %d %s (threshold is %d); check the resource lists everything it should",
				len(drop), profilesKind, engine.DefaultMaxPruneCount)
		}
	}
	send := declared
	for _, e := range existing {
		key := profileKey(e)
		if !slices.Contains(p.declared, key) && !slices.Contains(drop, key) {
			send = append(send, e)
		}
	}
	out, err := json.Marshal(send)
	if err != nil {
		return nil, fmt.Errorf("marshaling language profiles: %w", err)
	}
	p.send = string(out)
	return p, nil
}

func (p *bazarrProfiles) stored() []map[string]any {
	if p == nil {
		return nil
	}
	return p.existing
}

// saved records the profiles a successful save created and the ones it deleted.
func (p *bazarrProfiles) saved(owned *engine.Ownership) {
	for _, key := range p.declared {
		if !slices.Contains(p.present, key) {
			owned.Created(profilesKind, key)
		}
	}
	if p.prune {
		_ = owned.Prune(profilesKind, p.declared, p.present, func(string) error { return nil })
	}
}

// cleanup deletes the language profiles the operator created, for
// deletionPolicy delete.
func (r *BazarrConfigReconciler) cleanup(ctx context.Context, config *subtitlesv1alpha1.BazarrConfig) error {
	bc, _, err := r.bazarrClient(ctx, config)
	if err != nil {
		return err
	}
	existing, err := bc.GetLanguageProfiles(ctx)
	if err != nil {
		return err
	}
	present := make([]string, 0, len(existing))
	for _, e := range existing {
		present = append(present, profileKey(e))
	}
	var drop []string
	owned := engine.NewOwnership("bazarr", config.Status.ManagedResources)
	if err := owned.Cleanup(profilesKind, present, func(key string) error {
		drop = append(drop, key)
		return nil
	}); err != nil || len(drop) == 0 {
		return err
	}
	keep := slices.DeleteFunc(slices.Clone(existing), func(e map[string]any) bool { return slices.Contains(drop, profileKey(e)) })
	out, err := json.Marshal(keep)
	if err != nil {
		return err
	}
	return bc.PostForm(ctx, "/api/system/settings", url.Values{profilesField: {string(out)}})
}

func profileKey(profile map[string]any) string {
	switch id := profile["profileId"].(type) {
	case float64:
		return strconv.Itoa(int(id))
	case int:
		return strconv.Itoa(id)
	}
	return fmt.Sprint(profile["profileId"])
}
