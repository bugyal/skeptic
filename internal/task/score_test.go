package task

import (
	"errors"
	"testing"
)

func TestParseRewardText(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want float64
		bad  bool
	}{
		{name: "one", in: "1", want: 1},
		{name: "zero", in: "0", want: 0},
		{name: "fractional with newline", in: "0.5\n", want: 0.5},
		{name: "padded", in: "  1.0  ", want: 1},
		{name: "empty", in: "", bad: true},
		{name: "whitespace only", in: "  \n", bad: true},
		{name: "not a number", in: "PASSED", bad: true},
		{name: "above range", in: "1.5", bad: true},
		{name: "negative", in: "-1", bad: true},
		{name: "nan", in: "NaN", bad: true},
		{name: "inf", in: "Inf", bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRewardText([]byte(tc.in))
			if tc.bad {
				if err == nil {
					t.Fatalf("ParseRewardText(%q) = %v, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRewardText(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ParseRewardText(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestParseRewardJSON(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		key  string
		want float64
		bad  bool
	}{
		{name: "bare number", in: `1`, want: 1},
		{name: "bare float", in: `0.25`, want: 0.25},
		{name: "single key", in: `{"accuracy": 1.0}`, want: 1},
		{name: "reward key among many", in: `{"reward": 1.0, "style": 0.5}`, want: 1},
		{name: "explicit key", in: `{"a": 0.25, "b": 1.0}`, key: "b", want: 1},
		{name: "explicit key missing", in: `{"a": 1.0, "b": 1.0}`, key: "zz", bad: true},
		{name: "empty object", in: `{}`, bad: true},
		{name: "empty file", in: ``, bad: true},
		{name: "malformed", in: `{`, bad: true},
		{name: "string value", in: `{"reward": "1.0"}`, bad: true},
		{name: "array", in: `[1.0]`, bad: true},
		{name: "out of range", in: `{"reward": 2}`, bad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseRewardJSON([]byte(tc.in), tc.key)
			if tc.bad {
				if err == nil {
					t.Fatalf("ParseRewardJSON(%q) = %v, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseRewardJSON(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("ParseRewardJSON(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// A multi-key object with no "reward" key must surface as a distinct, typed
// error naming the keys -- not as a silently averaged score.
func TestParseRewardJSONAmbiguous(t *testing.T) {
	_, err := ParseRewardJSON([]byte(`{"style": 1.0, "speed": 0.5}`), "")
	var amb *ErrAmbiguousReward
	if !errors.As(err, &amb) {
		t.Fatalf("got %v (%T), want *ErrAmbiguousReward", err, err)
	}
	if len(amb.Keys) != 2 || amb.Keys[0] != "speed" || amb.Keys[1] != "style" {
		t.Fatalf("keys = %v, want sorted [speed style]", amb.Keys)
	}
}
