package check

import (
	"strings"
	"testing"
)

func TestParseRuleErrors(t *testing.T) {
	for _, s := range []string{"", "=> 0", "5", "> ", "> abc", "rows ~ x", "rows > abc", "~ ["} {
		if _, err := ParseRule(s); err == nil {
			t.Errorf("ParseRule(%q): expected error", s)
		}
	}
}

func TestEvalValue(t *testing.T) {
	cases := []struct {
		rule, value string
		ok          bool
	}{
		{"> 5", "6", true},
		{"> 5", "5", false},
		{">= 5", "5", true},
		{"< 10", "9.5", true},
		{"<= 1e9", "2e9", false},
		{"> 5e9", "6000000000", true},
		{"== 0", "0", true},
		{"== 0", "0.0", true}, // numeric comparison, not string
		{"== 0", "1", false},
		{"!= 0", "1", true},
		{"== ok", "ok", true},
		{"== ok", "ko", false},
		{"!= error", "fine", true},
		{"~ ^OPEN$", "OPEN", true},
		{"~ ^OPEN$", "OPENING", false},
		{"~ st.tus", "status", true},
	}
	for _, tc := range cases {
		r, err := ParseRule(tc.rule)
		if err != nil {
			t.Fatalf("ParseRule(%q): %v", tc.rule, err)
		}
		err = r.EvalValue(tc.value)
		if (err == nil) != tc.ok {
			t.Errorf("rule %q value %q: got err=%v, want ok=%v", tc.rule, tc.value, err, tc.ok)
		}
	}

	r, _ := ParseRule("> 5")
	if err := r.EvalValue("abc"); err == nil || !strings.Contains(err.Error(), "not a number") {
		t.Errorf("non-numeric value: got %v", err)
	}
}

func TestEvalRows(t *testing.T) {
	r, err := ParseRule("rows > 0")
	if err != nil {
		t.Fatal(err)
	}
	if !r.TargetsRows() {
		t.Fatal("expected TargetsRows")
	}
	if err := r.EvalRows(1); err != nil {
		t.Errorf("rows=1: %v", err)
	}
	if err := r.EvalRows(0); err == nil {
		t.Error("rows=0: expected error")
	}

	r2, err := ParseRule("rows == 5")
	if err != nil {
		t.Fatal(err)
	}
	if err := r2.EvalRows(5); err != nil {
		t.Errorf("rows=5: %v", err)
	}
}
