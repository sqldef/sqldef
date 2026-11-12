package pgcatalog

import (
	"slices"
	"testing"
)

func TestResolveFunction(t *testing.T) {
	r := NewResolver([]*Type{
		{Name: "public.posint", Kind: 'd', Base: "int4"},
		{Name: "public.mood", Category: CategoryEnum, Kind: 'e'},
	})
	tests := []struct {
		name   string
		args   []string
		params []string
		result string
	}{
		{"round", []string{"int4", "int4"}, []string{"numeric", "int4"}, "numeric"},
		{"round", []string{"numeric"}, []string{"numeric"}, "numeric"},
		{"sqrt", []string{"int4"}, []string{"float8"}, "float8"},
		{"lower", []string{"varchar"}, []string{"text"}, "text"},
		{"lower", []string{"bpchar"}, []string{"text"}, "text"},
		{"length", []string{"bpchar"}, []string{"bpchar"}, "int4"},
		{"length", []string{"varchar"}, []string{"text"}, "int4"},
		{"pg_table_size", []string{"text"}, []string{"regclass"}, "int8"},
		{"abs", []string{"public.posint"}, []string{"int4"}, "int4"},
		{"substr", []string{"varchar", "int4", "int4"}, []string{"text", "int4", "int4"}, "text"},
		{"date_trunc", []string{"unknown", "timestamptz"}, []string{"text", "timestamptz"}, "timestamptz"},
		{"now", nil, []string{}, "timestamptz"},
		{"array_length", []string{"_int4", "int4"}, []string{"_int4", "int4"}, "int4"},
		{"concat", []string{"int4", "unknown"}, []string{"any", "any"}, "text"},
		{"enum_first", []string{"public.mood"}, []string{"public.mood"}, "public.mood"},
	}
	for _, tt := range tests {
		call, ok := r.ResolveFunction(tt.name, tt.args)
		if !ok {
			t.Errorf("%s%v: not resolved", tt.name, tt.args)
			continue
		}
		if !slices.Equal(call.Params, tt.params) || call.Result != tt.result {
			t.Errorf("%s%v: got %v -> %s, want %v -> %s", tt.name, tt.args, call.Params, call.Result, tt.params, tt.result)
		}
	}
}

func TestResolveOperator(t *testing.T) {
	r := NewResolver([]*Type{{Name: "public.posint", Kind: 'd', Base: "int4"}})
	tests := []struct {
		name, left, right string
		params            []string
		result            string
	}{
		{">", "int4", "int4", []string{"int4", "int4"}, "bool"},
		{">", "int8", "int4", []string{"int8", "int4"}, "bool"},
		{">", "float8", "int4", []string{"float8", "float8"}, "bool"},
		{">", "numeric", "int4", []string{"numeric", "numeric"}, "bool"},
		{"+", "int4", "numeric", []string{"numeric", "numeric"}, "numeric"},
		{">", "public.posint", "int4", []string{"int4", "int4"}, "bool"},
		{"<>", "varchar", "unknown", []string{"text", "text"}, "bool"},
		{"<>", "text", "unknown", []string{"text", "text"}, "bool"},
		{"<>", "bpchar", "unknown", []string{"bpchar", "bpchar"}, "bool"},
		{"||", "varchar", "unknown", []string{"text", "text"}, "text"},
		{"~~", "varchar", "unknown", []string{"text", "text"}, "bool"},
		{"->>", "jsonb", "unknown", []string{"jsonb", "text"}, "text"},
		{"-", "", "int4", []string{"int4"}, "int4"},
		{"-", "timestamptz", "interval", []string{"timestamptz", "interval"}, "timestamptz"},
	}
	for _, tt := range tests {
		call, ok := r.ResolveOperator(tt.name, tt.left, tt.right)
		if !ok {
			t.Errorf("%s %s %s: not resolved", tt.left, tt.name, tt.right)
			continue
		}
		if !slices.Equal(call.Params, tt.params) || call.Result != tt.result {
			t.Errorf("%s %s %s: got %v -> %s, want %v -> %s", tt.left, tt.name, tt.right, call.Params, call.Result, tt.params, tt.result)
		}
	}
}

func TestCommonType(t *testing.T) {
	r := NewResolver([]*Type{{Name: "public.posint", Kind: 'd', Base: "int4"}})
	tests := []struct {
		types []string
		want  string
	}{
		{[]string{"int4", "int8"}, "int8"},
		{[]string{"int8", "int4"}, "int8"},
		{[]string{"int4", "numeric"}, "numeric"},
		{[]string{"varchar", "text"}, "varchar"},
		{[]string{"text", "varchar"}, "text"},
		{[]string{"unknown", "unknown"}, "text"},
		{[]string{"public.posint", "public.posint"}, "public.posint"},
		{[]string{"public.posint", "int4"}, "int4"},
		{[]string{"public.posint", "unknown"}, "int4"},
	}
	for _, tt := range tests {
		got, ok := r.CommonType(tt.types)
		if !ok || got != tt.want {
			t.Errorf("%v: got %s, %v, want %s", tt.types, got, ok, tt.want)
		}
	}
	if _, ok := r.CommonType([]string{"int4", "text"}); ok {
		t.Errorf("int4 and text have no common type")
	}
}
