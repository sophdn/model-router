package laddercfg

import "fmt"

// StarvedWindowFraction is the ratio below which a served context window counts as
// STARVED against the model's trained one, and the operator is told.
//
// A served window and a trained window are different kinds of number, and only one of
// them is a limit. The trained window is the model's capability. The served window is an
// operator flag — the inference server's context-size setting — and it can sit anywhere
// below it, silently, forever. At half or less, the rung is being starved by configuration
// rather than by capability, which is worth a line on stderr.
const StarvedWindowFraction = 0.5

// UsefulFloorWindow is the absolute size below which a served window cannot hold a real
// coding carry, whatever the model could theoretically do.
//
// It is the second half of the starved test, and it is what keeps the notice from becoming
// noise. A large model served at a deliberate, memory-bounded window is not misconfigured
// just because the model could go further — a model at 32768 against a trained 262144 is
// 12% of its window, and there is nothing to report. The number that matters is whether the
// window fits the work.
//
// 16384 comes from the measured shape of a coding duty on a typical ladder: fixed context
// is ~4,300 tokens (tool specs ~1,300 + injected skills ~2,000 + duty ~900) and a
// whole-file read is ~1,900, so a floor that can hold the fixed context plus three or four
// reads needs roughly 12,000 tokens. 16384 is the next power of two above that, with room
// for the floor-fit guard's 80% margin.
const UsefulFloorWindow = 16384

// StarvedWindowNotice returns an operator warning when a rung is served far below the
// window its model was trained for, or "" when it is not.
//
// This exists because that misconfiguration is silent, recoverable, and expensive. A common
// way to hit it: the local rung is swapped for a model trained on a much larger window (say
// 262,144 tokens), but the server keeps the small --ctx-size it carried for the previous
// model. Nothing reports the gap, and the consequence is that the fixed context plus a
// single whole-file read overflows the served window — so every turn off the local rung is
// a mandatory context-overflow escalation, and the free local tier does almost none of the
// work it is meant to do.
//
// The check does NOT fire on a legitimately small model, on a rung whose endpoint
// advertises no trained window, or on a cloud rung (which advertises neither). Unknown is
// silence, never a warning — a probe that returns nothing must not manufacture an alarm.
//
// Nor does it fire on a window that is merely below what the model COULD hold. Both halves
// must be true: far below the trained window AND too small to hold real work. Otherwise
// every deliberate VRAM-bounded choice would warn forever, and a line that always fires is
// a line nobody reads.
func StarvedWindowNotice(modelID string, served, trained int) string {
	if served <= 0 || trained <= 0 {
		return ""
	}
	if float64(served) >= float64(trained)*StarvedWindowFraction || served >= UsefulFloorWindow {
		return ""
	}
	return fmt.Sprintf(
		"model-router: %s is SERVED at %d tokens but TRAINED for %d (%.0f%% of its window). "+
			"This is a server flag, not a model limit — the server's --ctx-size. A starved floor "+
			"forces mandatory context-overflow escalation off the cheapest rung, which is the one "+
			"the work-share target wants doing the work.",
		modelID, served, trained, 100*float64(served)/float64(trained))
}
