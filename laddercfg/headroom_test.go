package laddercfg

import (
	"strings"
	"testing"
)

// The starved-window notice exists because that misconfiguration is silent, recoverable
// and expensive: swapping the local rung for a model trained on 262,144 tokens while the
// server keeps a leftover --ctx-size of 8192 turns every climb off the local rung into a
// mandatory context overflow.

// TestStarvedWindowNoticeFiresOnTheRealCase is that exact configuration.
func TestStarvedWindowNoticeFiresOnTheRealCase(t *testing.T) {
	got := StarvedWindowNotice("Qwen3.8-27B-Q4_K_M.gguf", 8192, 262144)
	if got == "" {
		t.Fatal("8192 served against 262144 trained is 3% of the window and must be reported")
	}
	for _, want := range []string{"Qwen3.8-27B-Q4_K_M.gguf", "8192", "262144", "3%", "--ctx-size"} {
		if !strings.Contains(got, want) {
			t.Errorf("the notice must be actionable; missing %q in: %s", want, got)
		}
	}
}

// TestStarvedWindowNoticeSilentWhenUnknown is the load-bearing negative. A probe that
// returns nothing must not manufacture an alarm — a cloud rung advertises neither number,
// and an adapter that cannot report a trained window is simply not asked.
func TestStarvedWindowNoticeSilentWhenUnknown(t *testing.T) {
	for _, c := range []struct{ served, trained int }{
		{0, 262144},  // no served window detected
		{8192, 0},    // endpoint advertises no trained window (every cloud rung)
		{0, 0},       // neither
		{-1, 262144}, // a nonsense probe result
	} {
		if got := StarvedWindowNotice("m", c.served, c.trained); got != "" {
			t.Errorf("served=%d trained=%d must be silent, got: %s", c.served, c.trained, got)
		}
	}
}

// TestStarvedWindowNoticeSilentOnADeliberateWindow is the case that would have turned the
// notice into noise. A floor served at 32768 is a deliberate, memory-bounded choice, and
// still only 12% of a 262144-token trained window. Warning about it forever would train a
// reader to ignore the line, so a window big enough to hold real work is silent however far
// below the model's ceiling it sits.
func TestStarvedWindowNoticeSilentOnADeliberateWindow(t *testing.T) {
	if got := StarvedWindowNotice("Qwen3.8-27B-Q4_K_M.gguf", 32768, 262144); got != "" {
		t.Errorf("32768 holds a coding carry and must be silent, got: %s", got)
	}
	if got := StarvedWindowNotice("m", UsefulFloorWindow, 262144); got != "" {
		t.Errorf("exactly at the useful floor must be silent, got: %s", got)
	}
	if got := StarvedWindowNotice("m", UsefulFloorWindow-1, 262144); got == "" {
		t.Error("one token below the useful floor must still fire")
	}
}

// TestStarvedWindowNoticeSilentOnAHealthyRung: a model served at or above half its trained
// window is doing what it can, and a legitimately small model must never read as starved.
func TestStarvedWindowNoticeSilentOnAHealthyRung(t *testing.T) {
	for _, c := range []struct {
		name            string
		served, trained int
	}{
		{"served at its full trained window", 32768, 32768},
		{"served above the ratio threshold", 20000, 32768},
		{"served exactly at the ratio threshold", 16384, 32768},
		{"a genuinely small model", 4096, 4096},
	} {
		if got := StarvedWindowNotice("m", c.served, c.trained); got != "" {
			t.Errorf("%s must be silent, got: %s", c.name, got)
		}
	}
}

// TestStarvedWindowNoticeFiresJustBelowTheThreshold pins the boundary from the other side,
// so the threshold is a tested edge rather than an assumed one.
func TestStarvedWindowNoticeFiresJustBelowTheThreshold(t *testing.T) {
	// Below half of trained AND below the useful floor — both halves, which is what the
	// notice now requires.
	if got := StarvedWindowNotice("m", 8191, 32768); got == "" {
		t.Error("below half the trained window and below the useful floor must fire")
	}
	// Below half of trained but big enough to work: silent.
	if got := StarvedWindowNotice("m", 16383, 262144); got == "" {
		t.Error("16383 is below the useful floor, so this must still fire")
	}
	if got := StarvedWindowNotice("m", 20000, 262144); got != "" {
		t.Errorf("20000 holds real work and must be silent however small the ratio, got: %s", got)
	}
}
