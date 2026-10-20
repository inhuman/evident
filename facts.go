package evident

import (
	"fmt"
	"reflect"
	"slices"
)

// Facts are the observed values at a decision point. Values come either from Go
// code directly or decoded from a structured log or JSON, so numbers of any Go
// type compare by value (int 3 == int64 3 == float64 3).
type Facts map[string]any

func (f Facts) Get(key string) (any, bool) {
	v, ok := f[key]
	return v, ok
}

func (f Facts) String(key string) string {
	s, _ := f[key].(string)
	return s
}

func (f Facts) Bool(key string) bool {
	b, _ := f[key].(bool)
	return b
}

// Int reads any integer or float kind, including time.Duration (as nanoseconds).
// Missing or non-numeric values read as 0; use Class.Require to make a key mandatory.
func (f Facts) Int(key string) int64 {
	n, _ := toInt(f[key])
	return n
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

// Equal compares fact values: numbers by value across Go types, everything else
// with reflect.DeepEqual (so slices and maps do not panic).
func Equal(a, b any) bool {
	na, nb := normalize(a), normalize(b)
	if fa, ok := na.(float64); ok {
		fb, ok := nb.(float64)
		return ok && fa == fb
	}
	return reflect.DeepEqual(na, nb)
}

// normalize maps every numeric kind to float64, slices to []any and maps with
// string keys to map[string]any, recursively: the shapes JSON decoding produces,
// so a structured model output compares with what a Go harness witnessed.
func normalize(v any) any {
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(rv.Uint())
	case reflect.Float32, reflect.Float64:
		return rv.Float()
	case reflect.Slice:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return v
		}
		out := make([]any, rv.Len())
		for i := range rv.Len() {
			out[i] = normalize(rv.Index(i).Interface())
		}
		return out
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return v
		}
		out := make(map[string]any, rv.Len())
		for it := rv.MapRange(); it.Next(); {
			out[it.Key().String()] = normalize(it.Value().Interface())
		}
		return out
	}
	return v
}

func toInt(v any) (int64, bool) {
	if v == nil {
		return 0, false
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return int64(rv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return int64(rv.Float()), true
	}
	return 0, false
}

// Is returns a When selector matching facts where key equals one of values.
func Is(key string, values ...any) func(Facts) bool {
	return func(f Facts) bool {
		v, ok := f[key]
		return ok && slices.ContainsFunc(values, func(want any) bool { return Equal(v, want) })
	}
}

// Always selects every observation of the requirement. Use it for an oracle
// class that checks all of them; pair it with a Domain so coverage is still
// enforced row by row.
func Always(Facts) bool { return true }

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

// Expect returns a Then predicate requiring key to be present and equal want.
func Expect(key string, want any) func(Facts) error {
	return func(f Facts) error {
		got, ok := f[key]
		if !ok {
			return fmt.Errorf("%s missing, want %v", key, want)
		}
		if !Equal(got, want) {
			return fmt.Errorf("%s = %v, want %v", key, got, want)
		}
		return nil
	}
}
