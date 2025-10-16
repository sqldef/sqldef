package util

import (
	"iter"
	"sort"
)

// TransformSlice applies the converter to each element in the input slice and returns a new slice.
func TransformSlice[T any, R any](in []T, converter func(T) R) []R {
	out := make([]R, len(in))
	for i, v := range in {
		out[i] = converter(v)
	}
	return out
}

// CanonicalMapIter returns an iterator that yields map entries in sorted key order.
// This ensures deterministic iteration over maps, which is useful for generating
// consistent output (e.g., DDL statements) regardless of Go's random map iteration order.
func CanonicalMapIter[T any](m map[string]T) iter.Seq2[string, T] {
	return func(yield func(string, T) bool) {
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, k := range keys {
			if !yield(k, m[k]) {
				return
			}
		}
	}
}
