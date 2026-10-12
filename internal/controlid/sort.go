package controlid

import (
	"sort"
	"strconv"
	"strings"
	"unicode"
)

func Compare(a, b string) int {
	aParts := tokenize(a)
	bParts := tokenize(b)

	for i := 0; i < len(aParts) && i < len(bParts); i++ {
		ap := aParts[i]
		bp := bParts[i]
		if ap.numeric && bp.numeric {
			an, _ := strconv.Atoi(ap.value)
			bn, _ := strconv.Atoi(bp.value)
			switch {
			case an < bn:
				return -1
			case an > bn:
				return 1
			case len(ap.value) < len(bp.value):
				return -1
			case len(ap.value) > len(bp.value):
				return 1
			}
			continue
		}

		av := strings.ToUpper(ap.value)
		bv := strings.ToUpper(bp.value)
		switch {
		case av < bv:
			return -1
		case av > bv:
			return 1
		}
	}

	switch {
	case len(aParts) < len(bParts):
		return -1
	case len(aParts) > len(bParts):
		return 1
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func Less(a, b string) bool {
	return Compare(a, b) < 0
}

func Sort(values []string) {
	sort.Slice(values, func(i, j int) bool {
		return Less(values[i], values[j])
	})
}

type token struct {
	value   string
	numeric bool
}

func tokenize(value string) []token {
	if value == "" {
		return nil
	}

	runes := []rune(value)
	out := make([]token, 0, len(runes))
	start := 0
	isDigit := unicode.IsDigit(runes[0])

	flush := func(end int) {
		out = append(out, token{
			value:   string(runes[start:end]),
			numeric: isDigit,
		})
	}

	for i := 1; i < len(runes); i++ {
		currentDigit := unicode.IsDigit(runes[i])
		if currentDigit == isDigit {
			continue
		}
		flush(i)
		start = i
		isDigit = currentDigit
	}
	flush(len(runes))

	return out
}
