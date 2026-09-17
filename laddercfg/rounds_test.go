package laddercfg

import (
	"testing"

	"github.com/sophdn/model-router/cost"
)

// The per-turn round budget used to be uniform across every rung, which spends the free
// resource as stingily as the expensive one: a local model call is fast and free, while a
// remote-rung call is slow and costs real money.

// TestRoundsForTierFallsWithRungCost is the acceptance criterion: floor > mid > strong for
// the canonical ladder, from one base.
func TestRoundsForTierFallsWithRungCost(t *testing.T) {
	const base = 24 // atomic-coding-chain's max_tool_rounds
	local := RoundsForTier(base, cost.TierLocal)
	mid := RoundsForTier(base, cost.TierMid)
	strong := RoundsForTier(base, cost.TierStrong)

	if !(local > mid && mid > strong) {
		t.Fatalf("budgets must fall with rung cost: local=%d mid=%d strong=%d", local, mid, strong)
	}
	if local != 48 || mid != 24 || strong != 12 {
		t.Errorf("base %d → local/mid/strong = %d/%d/%d, want 48/24/12", base, local, mid, strong)
	}
}

// TestRoundsForModelClassifiesRatherThanListing: the derivation reads cost.Classify, so a
// rung added purely by configuration inherits a sane budget with no table edit here. The
// model ids below are never named in this package.
func TestRoundsForModelClassifiesRatherThanListing(t *testing.T) {
	for _, c := range []struct {
		model string
		want  int
	}{
		{"Qwen3.8-27B-Q4_K_M.gguf", 20},
		{"google/gemini-3.1-flash-lite", 10},
		{"deepseek/deepseek-v3.2", 10},
		{"claude-opus-4-8", 5},
	} {
		if got := RoundsForModel(10, c.model); got != c.want {
			t.Errorf("RoundsForModel(10, %q) = %d, want %d", c.model, got, c.want)
		}
	}
}

// TestRoundsForUnknownRungGetsTheBase: an unclassified rung must be neither starved nor
// indulged. It gets exactly what the profile asked for, which is the only defensible
// default when nothing is known about its cost.
func TestRoundsForUnknownRungGetsTheBase(t *testing.T) {
	if got := RoundsForModel(9, "some-vendor/unheard-of-model"); got != 9 {
		t.Errorf("an unknown rung must get the base 9, got %d", got)
	}
}

// TestRoundsNeverStarvesAServingRung: a rung that is serving must get at least one round,
// or it cannot make a single tool call and the loop breaks out having done nothing.
func TestRoundsNeverStarvesAServingRung(t *testing.T) {
	if got := RoundsForTier(1, cost.TierStrong); got != 1 {
		t.Errorf("base 1 on the frugal rung must clamp to 1, got %d", got)
	}
}

// TestRoundsUnsetBaseStaysUnset: 0 means "the caller has no base", and must not become a
// budget of its own — the loop reads 0 as "use my own default".
func TestRoundsUnsetBaseStaysUnset(t *testing.T) {
	for _, tier := range []cost.Tier{cost.TierLocal, cost.TierMid, cost.TierStrong, cost.TierUnknown} {
		if got := RoundsForTier(0, tier); got != 0 {
			t.Errorf("RoundsForTier(0, %s) = %d, want 0", tier, got)
		}
		if got := RoundsForTier(-3, tier); got != 0 {
			t.Errorf("RoundsForTier(-3, %s) = %d, want 0", tier, got)
		}
	}
}
