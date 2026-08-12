package app

import (
	"strconv"
	"strings"
	"testing"
)

// benchPage builds a page of roughly the requested size. The real /controls
// page is ~45 KB served, so the 20 KB and 60 KB cases bracket production and
// 120 KB is the headroom case.
func benchPage(kb int) []byte {
	body := strings.Repeat("<div>Some representative page content here.</div>\n", kb*1024/50)
	return []byte("<html><head><style>x{}</style></head><body>" + body + "</body></html>")
}

// BenchmarkInjectGlobalUI guards the cost of the global-UI splice, which runs on
// every HTML response in the app.
//
// The original implementation chained six functions, each converting the payload
// to a string and lowercasing the whole document just to find a tag. On this
// machine that measured, for a 120 KB page:
//
//	4931 µs/op    2,025,219 B/op    16 allocs/op    ~25 MB/s
//
// The single-pass version measures:
//
//	48 µs/op      149,248 B/op     3 allocs/op    ~2550 MB/s
//
// If a future change reintroduces a whole-document copy — a strings.ToLower, a
// []byte→string conversion of the payload, or another chained pass — the
// bytes-per-op here will jump back toward a multiple of the page size and the
// throughput will collapse by two orders of magnitude. That is what this
// benchmark exists to make visible.
func BenchmarkInjectGlobalUI(b *testing.B) {
	for _, kb := range []int{20, 60, 120} {
		page := benchPage(kb)
		b.Run(strconv.Itoa(kb)+"KB", func(b *testing.B) {
			b.SetBytes(int64(len(page)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = injectGlobalUI(page, themeDark)
			}
		})
	}
}
