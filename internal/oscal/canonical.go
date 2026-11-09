package oscal

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"unicode/utf16"
)

// Canonical returns v as RFC 8785 (JSON Canonicalization Scheme) bytes: no
// insignificant whitespace, object members sorted by their UTF-16 code units,
// numbers in ECMAScript form, and strings escaped only where JSON requires it.
// Export bytes go through this so that the same data always gives the same
// bytes, and so that a later signature (OSCAL.md, signing) has one thing to
// sign. v may be any value encoding/json can marshal, or raw JSON bytes.
func Canonical(v any) ([]byte, error) {
	var raw []byte
	switch value := v.(type) {
	case json.RawMessage:
		raw = value
	case []byte:
		raw = value
	default:
		marshalled, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("canonical json: marshal: %w", err)
		}
		raw = marshalled
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return nil, fmt.Errorf("canonical json: decode: %w", err)
	}
	if dec.More() {
		return nil, errors.New("canonical json: trailing data after the value")
	}
	var buf bytes.Buffer
	if err := writeCanonical(&buf, tree); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeCanonical(buf *bytes.Buffer, v any) error {
	switch value := v.(type) {
	case nil:
		buf.WriteString("null")
	case bool:
		if value {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
	case json.Number:
		f, err := strconv.ParseFloat(string(value), 64)
		if err != nil {
			return fmt.Errorf("canonical json: number %q: %w", value, err)
		}
		s, err := es6Number(f)
		if err != nil {
			return err
		}
		buf.WriteString(s)
	case string:
		writeCanonicalString(buf, value)
	case []any:
		buf.WriteByte('[')
		for i, item := range value {
			if i > 0 {
				buf.WriteByte(',')
			}
			if err := writeCanonical(buf, item); err != nil {
				return err
			}
		}
		buf.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(value))
		for k := range value {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return utf16Less(keys[i], keys[j]) })
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeCanonicalString(buf, k)
			buf.WriteByte(':')
			if err := writeCanonical(buf, value[k]); err != nil {
				return err
			}
		}
		buf.WriteByte('}')
	default:
		return fmt.Errorf("canonical json: unexpected %T", v)
	}
	return nil
}

// es6Number formats a number as ECMAScript's Number.prototype.toString does,
// which is what RFC 8785 §3.2.2.3 requires. strconv's shortest round-trip
// digits are the same digits; only the exponent is written differently.
func es6Number(f float64) (string, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return "", fmt.Errorf("canonical json: %v has no JSON form", f)
	}
	if f == 0 {
		return "0", nil
	}
	format := byte('f')
	if abs := math.Abs(f); abs < 1e-6 || abs >= 1e21 {
		format = 'e'
	}
	s := strconv.FormatFloat(f, format, -1, 64)
	if format == 'e' {
		// Go writes at least two exponent digits ("1e-07"); ECMAScript does not.
		if n := len(s); n >= 4 && s[n-4] == 'e' && s[n-2] == '0' {
			s = s[:n-2] + s[n-1:]
		}
	}
	return s, nil
}

func writeCanonicalString(buf *bytes.Buffer, s string) {
	const hex = "0123456789abcdef"
	buf.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			buf.WriteString(`\"`)
		case '\\':
			buf.WriteString(`\\`)
		case '\b':
			buf.WriteString(`\b`)
		case '\f':
			buf.WriteString(`\f`)
		case '\n':
			buf.WriteString(`\n`)
		case '\r':
			buf.WriteString(`\r`)
		case '\t':
			buf.WriteString(`\t`)
		default:
			if r < 0x20 {
				buf.WriteString(`\u00`)
				buf.WriteByte(hex[r>>4])
				buf.WriteByte(hex[r&0xf])
				continue
			}
			buf.WriteRune(r)
		}
	}
	buf.WriteByte('"')
}

// utf16Less orders member names by their UTF-16 code units (RFC 8785 §3.2.3),
// which differs from Go's byte order for characters outside the BMP.
func utf16Less(a, b string) bool {
	ua, ub := utf16.Encode([]rune(a)), utf16.Encode([]rune(b))
	for i := 0; i < len(ua) && i < len(ub); i++ {
		if ua[i] != ub[i] {
			return ua[i] < ub[i]
		}
	}
	return len(ua) < len(ub)
}
