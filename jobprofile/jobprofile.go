// Package jobprofile carries the capability envelope stamped on a worker: the
// tier it runs on plus the tool scope it is allowed. It is a type-only leaf with
// no I/O — the registry that loads profiles from disk lives elsewhere, so the
// router and the ladder-sizing policy can read a profile's Tier without pulling in
// a config loader. That keeps the dependency graph of the routing core empty.
package jobprofile

import (
	"fmt"
	"sort"
	"strings"
)

// Tier is the model tier a profile runs on. Three-valued: local for mechanical
// work on a free on-device model, mid for routine reasoning on a cheap hosted
// model, and strong for escalation-gated hard reasoning on the frontier model.
// The router reads this to pick the profile's resting rung on the ladder.
type Tier string

// The three model tiers.
const (
	TierLocal  Tier = "local"
	TierMid    Tier = "mid"
	TierStrong Tier = "strong"
)

// validTiers is the closed tier taxonomy, used by Validate.
var validTiers = map[Tier]bool{TierLocal: true, TierMid: true, TierStrong: true}

// SurfaceScope is action-level tool scope: one tool surface plus the actions
// allowed on it. An empty Actions slice means the whole surface is allowed; a
// non-empty slice restricts the worker to exactly those actions.
type SurfaceScope struct {
	Surface string   `toml:"surface"`
	Actions []string `toml:"actions"`
}

// JobProfile is the capability envelope a worker runs under. The projection step
// builds the tool subset from Tools, and the router reads Tier to place the worker
// on the ladder.
type JobProfile struct {
	// Name is the unique profile id (kebab-case), referenced when spawning.
	Name string `toml:"name"`
	// Duty is the human-readable charge this profile fulfils.
	Duty string `toml:"duty"`
	// Signals are the keywords whose presence in a prompt selects this profile
	// when none is named explicitly. Empty means the profile is never auto-selected.
	Signals []string `toml:"signals"`
	// Tools is the action-level tool scope (the capability allow-list).
	Tools []SurfaceScope `toml:"tools"`
	// Skills are the discipline names injected into this worker's system prompt.
	Skills []string `toml:"skills"`
	// Tier is the model tier this profile runs on.
	Tier Tier `toml:"tier"`
	// EscalateOn optionally lists signals that bump the tier via the router
	// (for example "tool_error"). Empty means no profile-driven escalation.
	EscalateOn []string `toml:"escalate_on"`
}

// Validate checks a profile is well-formed: a name, a known tier, and no empty or
// duplicated surface in its tool scope. It returns the first problem found.
func (p JobProfile) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return fmt.Errorf("profile has no name")
	}
	if p.Tier == "" {
		return fmt.Errorf("profile %q has no tier", p.Name)
	}
	if !validTiers[p.Tier] {
		return fmt.Errorf("profile %q has unknown tier %q (want local|mid|strong)", p.Name, p.Tier)
	}
	seen := map[string]bool{}
	for _, t := range p.Tools {
		s := strings.TrimSpace(t.Surface)
		if s == "" {
			return fmt.Errorf("profile %q has a tool scope with no surface", p.Name)
		}
		if seen[s] {
			return fmt.Errorf("profile %q scopes surface %q more than once", p.Name, s)
		}
		seen[s] = true
	}
	return nil
}

// Surfaces returns the surface names this profile scopes, sorted — the set of tool
// surfaces the worker may reach at all.
func (p JobProfile) Surfaces() []string {
	out := make([]string, 0, len(p.Tools))
	for _, t := range p.Tools {
		out = append(out, t.Surface)
	}
	sort.Strings(out)
	return out
}
