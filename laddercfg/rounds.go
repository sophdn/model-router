package laddercfg

import "github.com/sophdn/model-router/cost"

// Rung-proportional tool-round budgets.
//
// The per-turn round budget used to be uniform across every rung: the profile set one
// number and the local floor, the mid rung and the frontier all got it. That inverts the
// cost-via-decomposition thesis. A local round is free and fast; a frontier round costs
// dollars and is slow. Spending the free resource as stingily as the expensive one is what
// makes a floor worker exhaust its rounds and climb, which is the opposite of the goal of
// keeping most of the work on the local rung.
//
// So the budget scales INVERSELY with the serving rung's cost. The multipliers are
// deliberately modest rather than dramatic: most of a trial's wall clock is worker model
// time, so rounds bought at a slow rung are bought in seconds as well as dollars. Generous
// at the floor, which is both free and the fastest rung; frugal at the frontier, which is
// neither.
//
// The derivation reads cost.Classify rather than a model list, so a rung added purely by
// configuration inherits a sane budget with no table edit.

// Round-budget multipliers per tier, applied to the profile's (or the default) base.
const (
	// LocalRoundMultiplier is generous: the rung is free, it is the fastest measured
	// rung, and it is the one the work-share target wants doing the work.
	LocalRoundMultiplier = 2.0
	// MidRoundMultiplier leaves the mid rung on the base budget — it is the reference
	// the profile author was thinking of when they set the number.
	MidRoundMultiplier = 1.0
	// StrongRoundMultiplier is frugal. A frontier rung that cannot finish inside half the
	// base budget is not going to be rescued by more rounds; it is going to be expensive.
	StrongRoundMultiplier = 0.5
	// UnknownRoundMultiplier is the base. An unclassified rung must not be silently
	// starved or silently indulged — it gets exactly what the profile asked for.
	UnknownRoundMultiplier = 1.0
)

// RoundsForTier scales a base per-turn round budget by the serving tier.
//
// The result is never below 1: a rung that is serving must be allowed at least one round,
// or it cannot make a single tool call and the loop would break out before doing anything.
// A base of 0 or less yields 0, which callers read as "unset, use your own default".
func RoundsForTier(base int, tier cost.Tier) int {
	if base <= 0 {
		return 0
	}
	n := int(float64(base) * roundMultiplier(tier))
	if n < 1 {
		return 1
	}
	return n
}

// RoundsForModel is RoundsForTier keyed on a model id, classified through cost.Classify —
// the single home for model→tier knowledge, so no parallel table is introduced here.
func RoundsForModel(base int, model string) int {
	return RoundsForTier(base, cost.Classify(model))
}

// roundMultiplier returns the tier's budget multiplier.
func roundMultiplier(tier cost.Tier) float64 {
	switch tier {
	case cost.TierLocal:
		return LocalRoundMultiplier
	case cost.TierMid:
		return MidRoundMultiplier
	case cost.TierStrong:
		return StrongRoundMultiplier
	}
	return UnknownRoundMultiplier
}
