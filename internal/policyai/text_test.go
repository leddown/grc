package policyai

import "testing"

func TestFindQuoteNormalizesAndMapsBack(t *testing.T) {
	text := "Staff “will”  review access — quarterly.\nCafé staff too."
	cases := []struct {
		quote, want, reason string
	}{
		{`"will" review access - quarterly.`, "“will”  review access — quarterly.", ""},
		{"Café staff", "Café staff", ""},
		{"aff", "", "the quoted text appears more than once in the block"},
		{"auditors", "", "the quoted text is not in the block"},
		{"  ", "", "the quote is empty"},
	}
	for _, tc := range cases {
		from, to, reason := findQuote(text, tc.quote)
		if reason != tc.reason {
			t.Errorf("%q: reason %q, want %q", tc.quote, reason, tc.reason)
			continue
		}
		if reason == "" && text[from:to] != tc.want {
			t.Errorf("%q maps back to %q, want %q", tc.quote, text[from:to], tc.want)
		}
	}
}

func TestOverlapsPending(t *testing.T) {
	b := block{Text: "Staff will review access.", Protected: [][2]int{{6, 10}}, Inserts: []int{17}}
	for _, tc := range []struct {
		from, to int
		want     bool
	}{{0, 5, false}, {0, 7, true}, {11, 17, false}, {11, 18, true}, {17, 24, false}} {
		if got := b.overlapsPending(tc.from, tc.to); got != tc.want {
			t.Errorf("[%d,%d) overlaps=%v want %v", tc.from, tc.to, got, tc.want)
		}
	}
}
