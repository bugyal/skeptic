package task

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// ErrAmbiguousReward is returned when a reward artifact parses but does not
// identify a single score. Skeptic refuses to average or otherwise invent one:
// a tool whose claim is that a benchmark is lying cannot itself guess.
type ErrAmbiguousReward struct {
	Keys []string
}

func (e *ErrAmbiguousReward) Error() string {
	return fmt.Sprintf("reward object has %d keys and no \"reward\" key (%s); "+
		"set score.reward_key to choose one", len(e.Keys), strings.Join(e.Keys, ", "))
}

// ParseRewardText reads a bare float, the form Harbor writes to reward.txt.
func ParseRewardText(b []byte) (float64, error) {
	s := strings.TrimSpace(string(b))
	if s == "" {
		return 0, fmt.Errorf("reward file is empty")
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("reward file is not a number: %q", truncate(s, 64))
	}
	return v, validateScore(v)
}

// ParseRewardJSON reads reward.json, which Harbor permits to be either a bare
// number or an object of named rewards. Harbor never reduces the object to a
// scalar, so the reduction rule here is Skeptic's own; see docs/decisions.md D2.
func ParseRewardJSON(b []byte, key string) (float64, error) {
	if len(strings.TrimSpace(string(b))) == 0 {
		return 0, fmt.Errorf("reward file is empty")
	}

	var any interface{}
	if err := json.Unmarshal(b, &any); err != nil {
		return 0, fmt.Errorf("reward file is not valid JSON: %w", err)
	}

	switch v := any.(type) {
	case float64:
		return v, validateScore(v)

	case map[string]interface{}:
		if len(v) == 0 {
			return 0, fmt.Errorf("reward object is empty")
		}
		// An explicit key wins, so an operator can disambiguate without
		// editing the benchmark.
		if key != "" {
			raw, ok := v[key]
			if !ok {
				return 0, fmt.Errorf("reward object has no key %q (have: %s)",
					key, strings.Join(sortedKeys(v), ", "))
			}
			return numeric(raw, key)
		}
		if len(v) == 1 {
			for k, raw := range v {
				return numeric(raw, k)
			}
		}
		// Harbor's own text parser normalises to {"reward": value}, so that
		// key is the closest thing to a convention upstream has.
		if raw, ok := v["reward"]; ok {
			return numeric(raw, "reward")
		}
		return 0, &ErrAmbiguousReward{Keys: sortedKeys(v)}

	default:
		return 0, fmt.Errorf("reward file must be a number or an object, got %T", any)
	}
}

func numeric(raw interface{}, key string) (float64, error) {
	f, ok := raw.(float64)
	if !ok {
		return 0, fmt.Errorf("reward %q is not a number: %v", key, raw)
	}
	return f, validateScore(f)
}

// validateScore rejects anything that cannot be a score. Non-finite values are
// rejected for the same reason Harbor's verifier rejects them: JSON parses
// NaN and 1e309 into floats that would silently poison every comparison.
func validateScore(v float64) error {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return fmt.Errorf("reward is not finite: %v", v)
	}
	if v < 0 || v > 1 {
		return fmt.Errorf("reward %v is outside [0,1]", v)
	}
	return nil
}

func sortedKeys(m map[string]interface{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
