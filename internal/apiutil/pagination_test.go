package apiutil

import "testing"

type fakeQuery map[string]string

func (f fakeQuery) Query(key string) string { return f[key] }

// A page number large enough to overflow (page-1)*perPage used to wrap negative
// and panic PaginateSlice with a negative slice bound. Reachable from every
// paginated endpoint by query string, so it was a one-request 500.
func TestPaginationSurvivesOverflowingPageNumbers(t *testing.T) {
	items := make([]int, 10)

	for _, raw := range []string{
		"9223372036854775807", // math.MaxInt64
		"9223372036854775806",
		"4611686018427387904", // MaxInt64/2
		"1000000000000",
	} {
		params := ParsePagination(fakeQuery{"page": raw, "per_page": "500"})
		if params.Page > maxPage {
			t.Errorf("page %s not clamped: got %d", raw, params.Page)
		}
		got, total := PaginateSlice(items, params) // must not panic
		if len(got) != 0 {
			t.Errorf("page %s: expected an empty page past the end, got %d items", raw, len(got))
		}
		if total != len(items) {
			t.Errorf("page %s: total = %d, want %d", raw, total, len(items))
		}
	}
}

// PaginationParams is exported, so a hand-built value has to be survivable too.
func TestPaginateSliceRejectsHandBuiltNonsense(t *testing.T) {
	items := make([]int, 10)

	cases := map[string]PaginationParams{
		"negative page":     {Page: -5, PerPage: 10, Enabled: true},
		"zero page":         {Page: 0, PerPage: 10, Enabled: true},
		"negative per page": {Page: 1, PerPage: -10, Enabled: true},
		"overflowing page":  {Page: 1<<62 + 1, PerPage: 500, Enabled: true},
	}
	for name, params := range cases {
		got, total := PaginateSlice(items, params) // must not panic
		if len(got) != 0 {
			t.Errorf("%s: expected empty page, got %d items", name, len(got))
		}
		if total != len(items) {
			t.Errorf("%s: total = %d, want %d", name, total, len(items))
		}
	}
}

func TestPaginationNormalCases(t *testing.T) {
	items := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}

	// Absent parameters mean "no pagination", and the full slice comes back.
	all, total := PaginateSlice(items, ParsePagination(fakeQuery{}))
	if len(all) != 10 || total != 10 {
		t.Fatalf("unpaginated = %d items (total %d), want 10/10", len(all), total)
	}

	first, _ := PaginateSlice(items, ParsePagination(fakeQuery{"page": "1", "per_page": "3"}))
	if len(first) != 3 || first[0] != 0 || first[2] != 2 {
		t.Errorf("page 1 = %v, want [0 1 2]", first)
	}

	// A partial final page must be truncated to the data, not to per_page.
	last, _ := PaginateSlice(items, ParsePagination(fakeQuery{"page": "4", "per_page": "3"}))
	if len(last) != 1 || last[0] != 9 {
		t.Errorf("final page = %v, want [9]", last)
	}

	past, _ := PaginateSlice(items, ParsePagination(fakeQuery{"page": "99", "per_page": "3"}))
	if len(past) != 0 {
		t.Errorf("page past the end = %v, want empty", past)
	}
}

func TestPerPageIsCapped(t *testing.T) {
	params := ParsePagination(fakeQuery{"page": "1", "per_page": "100000"})
	if params.PerPage != maxPerPage {
		t.Errorf("per_page = %d, want it capped at %d", params.PerPage, maxPerPage)
	}
}

// Unparseable values fall back to the defaults rather than disabling paging,
// because a caller that sent page/per_page at all expects a paged envelope.
func TestUnparseableValuesFallBackToDefaults(t *testing.T) {
	params := ParsePagination(fakeQuery{"page": "abc", "per_page": "xyz"})
	if !params.Enabled {
		t.Fatal("pagination should stay enabled when the values are present but junk")
	}
	if params.Page != 1 || params.PerPage != 50 {
		t.Errorf("got page=%d per_page=%d, want 1/50", params.Page, params.PerPage)
	}
}
