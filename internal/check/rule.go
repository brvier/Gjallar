package check

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Rule is a tiny assertion language shared by the SQL and Prometheus checkers:
//
//	"> 5"  ">= 0"  "< 10"  "<= 1e9"   numeric comparison
//	"== ok"  "!= error"               numeric if both sides parse, else string
//	"~ ^OPEN$"                        regex match
//	"rows > 0"                        applies to the row count instead of the value
type Rule struct {
	raw    string
	rows   bool // rule targets the row count
	op     string
	arg    string
	numArg float64
	numOK  bool
	re     *regexp.Regexp
}

var ruleOps = []string{">=", "<=", "==", "!=", ">", "<", "~"}

func ParseRule(s string) (*Rule, error) {
	r := &Rule{raw: strings.TrimSpace(s)}
	expr := r.raw
	if rest, ok := strings.CutPrefix(expr, "rows"); ok {
		r.rows = true
		expr = strings.TrimSpace(rest)
	}
	for _, op := range ruleOps {
		if rest, ok := strings.CutPrefix(expr, op); ok {
			r.op = op
			r.arg = strings.TrimSpace(rest)
			break
		}
	}
	if r.op == "" {
		return nil, fmt.Errorf("rule %q: expected an operator (%s)", s, strings.Join(ruleOps, " "))
	}
	if r.arg == "" {
		return nil, fmt.Errorf("rule %q: missing argument after %q", s, r.op)
	}
	if n, err := strconv.ParseFloat(r.arg, 64); err == nil {
		r.numArg, r.numOK = n, true
	}
	switch r.op {
	case ">", ">=", "<", "<=":
		if !r.numOK {
			return nil, fmt.Errorf("rule %q: %q is not a number", s, r.arg)
		}
	case "~":
		if r.rows {
			return nil, fmt.Errorf("rule %q: regex cannot apply to a row count", s)
		}
		re, err := regexp.Compile(r.arg)
		if err != nil {
			return nil, fmt.Errorf("rule %q: %w", s, err)
		}
		r.re = re
	}
	if r.rows && !r.numOK {
		return nil, fmt.Errorf("rule %q: row count rules need a numeric argument", s)
	}
	return r, nil
}

func (r *Rule) TargetsRows() bool { return r.rows }

// EvalRows checks the rule against a row count.
func (r *Rule) EvalRows(n int) error {
	if ok := compareNum(float64(n), r.op, r.numArg); !ok {
		return fmt.Errorf("row count %d does not satisfy %q", n, r.raw)
	}
	return nil
}

// EvalValue checks the rule against a single value rendered as string.
func (r *Rule) EvalValue(v string) error {
	fail := func() error { return fmt.Errorf("value %q does not satisfy %q", v, r.raw) }
	switch r.op {
	case "~":
		if !r.re.MatchString(v) {
			return fail()
		}
	case "==", "!=":
		if r.numOK {
			if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
				if !compareNum(n, r.op, r.numArg) {
					return fail()
				}
				return nil
			}
		}
		if (v == r.arg) != (r.op == "==") {
			return fail()
		}
	default: // > >= < <=
		n, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return fmt.Errorf("value %q is not a number (rule %q)", v, r.raw)
		}
		if !compareNum(n, r.op, r.numArg) {
			return fail()
		}
	}
	return nil
}

func compareNum(a float64, op string, b float64) bool {
	switch op {
	case ">":
		return a > b
	case ">=":
		return a >= b
	case "<":
		return a < b
	case "<=":
		return a <= b
	case "==":
		return a == b
	case "!=":
		return a != b
	}
	return false
}
