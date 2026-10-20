package evident

import (
	"fmt"
	"slices"
)

// Facts are the observed values at a decision point. Values come either from Go
// code directly or decoded from a structured log, so numeric and list accessors
// accept the shapes both produce.
type Facts map[string]any

func (f Facts) String(key string) string {
	s, _ := f[key].(string)
	return s
}

func (f Facts) Bool(key string) bool {
	b, _ := f[key].(bool)
	return b
}

func (f Facts) Int(key string) int64 {
	switch n := f[key].(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case int32:
		return int64(n)
	case uint16:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}

func (f Facts) Strings(key string) []string {
	switch v := f[key].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, e := range v {
			out = append(out, fmt.Sprint(e))
		}
		return out
	}
	return nil
}

func (f Facts) Has(key string) bool {
	_, ok := f[key]
	return ok
}

// Is returns a When selector matching facts where key equals one of values.
func Is(key string, values ...any) func(Facts) bool {
	return func(f Facts) bool {
		v, ok := f[key]
		return ok && slices.Contains(values, v)
	}
}

// All combines When selectors with logical AND.
func All(sel ...func(Facts) bool) func(Facts) bool {
	return func(f Facts) bool {
		for _, s := range sel {
			if !s(f) {
				return false
			}
		}
		return true
	}
}

// Expect returns a Then predicate requiring key to equal want.
func Expect(key string, want any) func(Facts) error {
	return func(f Facts) error {
		if got := f[key]; got != want {
			return fmt.Errorf("%s = %v, want %v", key, got, want)
		}
		return nil
	}
}
