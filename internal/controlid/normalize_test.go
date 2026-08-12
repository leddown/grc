package controlid

import (
	"reflect"
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "AC-1", want: "AC-1"},
		{in: "ac-2.1", want: "AC-2(1)"},
		{in: " AC-3(2) ", want: "AC-3(2)"},
		{in: "", want: ""},
	}

	for _, tc := range tests {
		got := Normalize(tc.in)
		if got != tc.want {
			t.Fatalf("Normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFamily(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "AC-1", want: "AC"},
		{in: "SI-12(3)", want: "SI"},
		{in: "", want: ""},
	}

	for _, tc := range tests {
		got := Family(tc.in)
		if got != tc.want {
			t.Fatalf("Family(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSort_NaturalControlOrder(t *testing.T) {
	values := []string{"AU-10", "AU-2", "AU-1", "AU-3(1)", "AU-3"}
	Sort(values)
	want := []string{"AU-1", "AU-2", "AU-3", "AU-3(1)", "AU-10"}
	if !reflect.DeepEqual(values, want) {
		t.Fatalf("Sort(values)=%v want=%v", values, want)
	}
}
