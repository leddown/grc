package oscal

import (
	"math"
	"testing"
)

// The example in RFC 8785 §3.2.2.
func TestCanonicalRFC8785Example(t *testing.T) {
	in := `{
  "numbers": [333333333.33333329, 1E30, 4.50, 2e-3, 0.000000000000000000000000001],
  "string": "\u20ac$\u000F\u000aA'\u0042\u0022\u005c\\\"\/",
  "literals": [null, true, false]
}`
	want := `{"literals":[null,true,false],"numbers":[333333333.3333333,1e+30,4.5,0.002,1e-27],"string":"€$\u000f\nA'B\"\\\\\"/"}`
	got, err := Canonical([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// The member-ordering example in RFC 8785 §3.2.3: UTF-16 code units, so the
// emoji (a surrogate pair starting 0xD83D) sorts before U+FB33.
func TestCanonicalSortsByUTF16CodeUnits(t *testing.T) {
	in := `{
  "\u20ac": "Euro Sign",
  "\r": "Carriage Return",
  "\ufb33": "Hebrew Letter Dalet With Dagesh",
  "1": "One",
  "\ud83d\ude00": "Emoji: Grinning Face",
  "\u0080": "Control",
  "\u00f6": "Latin Small Letter O With Diaeresis"
}`
	want := "{\"\\r\":\"Carriage Return\",\"1\":\"One\",\"\u0080\":\"Control\",\"\u00f6\":\"Latin Small Letter O With Diaeresis\",\"\u20ac\":\"Euro Sign\",\"\U0001F600\":\"Emoji: Grinning Face\",\"\ufb33\":\"Hebrew Letter Dalet With Dagesh\"}"
	got, err := Canonical([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

// The number table in RFC 8785 Appendix B.
func TestCanonicalNumbersFollowECMAScript(t *testing.T) {
	cases := []struct {
		bits uint64
		want string
	}{
		{0x0000000000000000, "0"},
		{0x8000000000000000, "0"},
		{0x0000000000000001, "5e-324"},
		{0x8000000000000001, "-5e-324"},
		{0x7fefffffffffffff, "1.7976931348623157e+308"},
		{0xffefffffffffffff, "-1.7976931348623157e+308"},
		{0x4340000000000000, "9007199254740992"},
		{0xc340000000000000, "-9007199254740992"},
		{0x4430000000000000, "295147905179352830000"},
		{0x44b52d02c7e14af5, "9.999999999999997e+22"},
		{0x44b52d02c7e14af6, "1e+23"},
		{0x44b52d02c7e14af7, "1.0000000000000001e+23"},
		{0x444b1ae4d6e2ef4e, "999999999999999700000"},
		{0x444b1ae4d6e2ef4f, "999999999999999900000"},
		{0x444b1ae4d6e2ef50, "1e+21"},
		{0x3eb0c6f7a0b5ed8c, "9.999999999999997e-7"},
		{0x3eb0c6f7a0b5ed8d, "0.000001"},
		{0x41b3de4355555553, "333333333.3333332"},
		{0x41b3de4355555554, "333333333.33333325"},
		{0x41b3de4355555555, "333333333.3333333"},
		{0x41b3de4355555556, "333333333.3333334"},
		{0x41b3de4355555557, "333333333.33333343"},
		{0xbecbf647612f3696, "-0.0000033333333333333333"},
		{0x43143ff3c1cb0959, "1424953923781206.2"},
	}
	for _, c := range cases {
		got, err := es6Number(math.Float64frombits(c.bits))
		if err != nil {
			t.Fatalf("%#x: %v", c.bits, err)
		}
		if got != c.want {
			t.Errorf("%#x: got %s want %s", c.bits, got, c.want)
		}
	}
	if _, err := es6Number(math.NaN()); err == nil {
		t.Error("NaN must be refused")
	}
	if _, err := es6Number(math.Inf(1)); err == nil {
		t.Error("Infinity must be refused")
	}
}

func TestCanonicalOfAStructIsStableAndLeavesHTMLAlone(t *testing.T) {
	type doc struct {
		Z string            `json:"z"`
		A map[string]string `json:"a"`
	}
	v := doc{Z: "<b>&</b>", A: map[string]string{"y": "1", "x": "2"}}
	first, err := Canonical(v)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := Canonical(v)
	if string(first) != string(second) {
		t.Fatal("two canonical forms of the same value differ")
	}
	if want := `{"a":{"x":"2","y":"1"},"z":"<b>&</b>"}`; string(first) != want {
		t.Fatalf("got %s want %s", first, want)
	}
	if _, err := Canonical([]byte(`{"a":1} {"b":2}`)); err == nil {
		t.Fatal("trailing data must be refused")
	}
}
