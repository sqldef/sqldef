package schema

import "testing"

func TestIntervalCanonicalization(t *testing.T) {
	// input => expected PG interval_out (captured from postgres:18)
	cases := map[string]string{
		"1 year":              "1 year",
		"1 day":               "1 day",
		"1 mon":               "1 mon",
		"1 week":              "7 days",
		"90 minutes":          "01:30:00",
		"1 hour":              "01:00:00",
		"1 second":            "00:00:01",
		"1.5 hours":           "01:30:00",
		"2 years 3 months":    "2 years 3 mons",
		"1 day 2 hours":       "1 day 02:00:00",
		"1 year 1 day 1 hour": "1 year 1 day 01:00:00",
		"0":                   "00:00:00",
		"36 hours":            "36:00:00",
		"100 seconds":         "00:01:40",
		"1.5 days":            "1 day 12:00:00",
		"0.5 seconds":         "00:00:00.5",
		"3 mons":              "3 mons",
		"1-2":                 "1 year 2 mons",
		"1 decade":            "10 years",
		"1 century":           "100 years",
		// typed-literal spellings (value + singular unit)
		"1 minute":  "00:01:00",
		"90 minute": "01:30:00",
	}
	for in, want := range cases {
		got, ok := canonicalizeIntervalText(in)
		if !ok {
			t.Errorf("interval %q: not parsed", in)
			continue
		}
		if got != want {
			t.Errorf("interval %q => %q, want %q", in, got, want)
		}
	}
}

func TestMoneyCanonicalization(t *testing.T) {
	// PG rounds money half away from zero (verified: '1.005'::money = $1.01).
	cases := map[string]string{
		"1.00":      "1.00",
		"$1.00":     "1.00",
		"$1,000.00": "1000.00",
		"1":         "1.00",
		"1.5":       "1.50",
		"-$1.00":    "-1.00",
		"($1.00)":   "-1.00",
		"0":         "0.00",
		"1.999":     "2.00",
		"1.005":     "1.01",
		"1.994":     "1.99",
		"1.995":     "2.00",
		"0.001":     "0.00",
		"-1.005":    "-1.01",
	}
	for in, want := range cases {
		got, ok := canonicalizeMoneyText(in)
		if !ok {
			t.Errorf("money %q: not parsed", in)
			continue
		}
		if got != want {
			t.Errorf("money %q => %q, want %q", in, got, want)
		}
	}
	// Malformed input must be declined, not silently repaired.
	for _, in := range []string{"1.2.3", "", "$", "abc"} {
		if got, ok := canonicalizeMoneyText(in); ok {
			t.Errorf("money %q: expected not-parsed, got %q", in, got)
		}
	}
}

func TestIntervalCanonicalizationRejectsNonFinite(t *testing.T) {
	for _, in := range []string{"infinity", "-infinity", "nan"} {
		if got, ok := canonicalizeIntervalText(in); ok {
			t.Errorf("interval %q: expected not-parsed, got %q", in, got)
		}
	}
}

func TestTruncateNumericText(t *testing.T) {
	cases := map[string]string{"1.5": "1", "1": "1", "90": "90", "-1.5": "-1", "1.999": "1"}
	for in, want := range cases {
		if got := truncateNumericText(in); got != want {
			t.Errorf("truncateNumericText(%q) = %q, want %q", in, got, want)
		}
	}
}
