package check

import (
	"testing"
)

func TestCombineSteps(t *testing.T) {
	steps := []map[string]float64{
		{"reward": 1, "style": 1},
		{"reward": 0.5},
	}
	mean := combineSteps(steps, "mean")
	// A key a step did not report counts 0 for that step, as upstream.
	if mean["reward"] != 0.75 || mean["style"] != 0.5 {
		t.Errorf("mean = %v, want reward 0.75, style 0.5", mean)
	}
	final := combineSteps(steps, "final")
	if len(final) != 1 || final["reward"] != 0.5 {
		t.Errorf("final = %v, want the last step's alone", final)
	}
	if combineSteps(nil, "mean") != nil {
		t.Error("no steps must combine to nothing, not a zero score")
	}
}

func TestBelowMinReward(t *testing.T) {
	for _, tc := range []struct {
		name    string
		rewards map[string]float64
		min     map[string]float64
		key     string
		below   bool
	}{
		{"no gate", map[string]float64{"reward": 0}, nil, "", false},
		{"meets", map[string]float64{"reward": 0.5}, map[string]float64{"reward": 0.5}, "", false},
		{"under", map[string]float64{"reward": 0.4}, map[string]float64{"reward": 0.5}, "reward", true},
		// A gated key the step never wrote is a miss, not a pass.
		{"missing key", map[string]float64{"reward": 1}, map[string]float64{"tests": 0}, "tests", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			key, below := belowMinReward(tc.rewards, tc.min)
			if key != tc.key || below != tc.below {
				t.Errorf("= %q, %v; want %q, %v", key, below, tc.key, tc.below)
			}
		})
	}
}

func TestFormatRewards(t *testing.T) {
	if got := formatRewards(map[string]float64{"reward": 1}); got != "1.00" {
		t.Errorf("single reward = %q", got)
	}
	if got := formatRewards(map[string]float64{"b": 0, "a": 1}); got != "{a=1.00 b=0.00}" {
		t.Errorf("keys = %q", got)
	}
}
