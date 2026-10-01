package schema

import (
	"testing"
)

// A type modifier that is not a length has to reach the generated DDL. Without
// it, a column declared as geometry(Point,4326) is added as a plain geometry:
// the constraint the schema asked for is silently dropped.
func TestGenerateDataTypeKeepsTypeModifier(t *testing.T) {
	g := &Generator{dialect: dialect{mode: GeneratorModePostgres}}

	tests := []struct {
		name   string
		column Column
		want   string
	}{
		{
			name:   "shape and srid",
			column: Column{typeName: "geometry", typeModifier: "Point,4326"},
			want:   "geometry(Point,4326)",
		},
		{
			name:   "shape only",
			column: Column{typeName: "geometry", typeModifier: "Point"},
			want:   "geometry(Point)",
		},
		{
			name:   "array keeps its suffix",
			column: Column{typeName: "geometry", typeModifier: "Point,4326", array: true},
			want:   "geometry(Point,4326)[]",
		},
		{
			name:   "no modifier is unchanged",
			column: Column{typeName: "geometry"},
			want:   "geometry",
		},
		{
			name:   "a length is still a length",
			column: Column{typeName: "numeric", length: &Value{valueType: ValueTypeInt, intVal: 10, raw: "10"}, scale: &Value{valueType: ValueTypeInt, intVal: 2, raw: "2"}},
			want:   "numeric(10, 2)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := g.generateDataType(tt.column); got != tt.want {
				t.Errorf("generateDataType() = %q, want %q", got, tt.want)
			}
		})
	}
}

// Two columns of the same type name differ when their modifiers differ:
// geometry(Point,4326) and geometry(MultiPolygon,3857) are both "geometry".
// Treating them as equal leaves an SRID or shape change silently unapplied.
func TestHaveSameDataTypeComparesTypeModifier(t *testing.T) {
	g := &Generator{dialect: dialect{mode: GeneratorModePostgres}}

	tests := []struct {
		name    string
		current Column
		desired Column
		same    bool
	}{
		{
			name:    "identical modifiers",
			current: Column{typeName: "geometry", typeModifier: "Point,4326"},
			desired: Column{typeName: "geometry", typeModifier: "Point,4326"},
			same:    true,
		},
		{
			name:    "different srid",
			current: Column{typeName: "geometry", typeModifier: "Point,4326"},
			desired: Column{typeName: "geometry", typeModifier: "Point,3857"},
			same:    false,
		},
		{
			name:    "different shape",
			current: Column{typeName: "geometry", typeModifier: "Point,4326"},
			desired: Column{typeName: "geometry", typeModifier: "PointZ,4326"},
			same:    false,
		},
		{
			name:    "modifier added",
			current: Column{typeName: "geometry"},
			desired: Column{typeName: "geometry", typeModifier: "Point,4326"},
			same:    false,
		},
		{
			name:    "modifier removed",
			current: Column{typeName: "geometry", typeModifier: "Point,4326"},
			desired: Column{typeName: "geometry"},
			same:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := g.haveSameDataType(tt.current, tt.desired); got != tt.same {
				t.Errorf("haveSameDataType() = %v, want %v", got, tt.same)
			}
		})
	}
}
