package apiutil

import (
	"strconv"
	"strings"
)

const (
	// maxPerPage caps a single page of results.
	maxPerPage = 500
	// maxPage caps the requested page number. The product maxPage*maxPerPage
	// stays far inside int64, so the offset arithmetic in PaginateSlice cannot
	// overflow no matter what a caller sends.
	maxPage = 1 << 30
)

type PaginationParams struct {
	Page    int
	PerPage int
	Enabled bool
}

type queryReader interface {
	Query(string) string
}

func ParsePagination(q queryReader) PaginationParams {
	pageRaw := strings.TrimSpace(q.Query("page"))
	perPageRaw := strings.TrimSpace(q.Query("per_page"))
	if pageRaw == "" && perPageRaw == "" {
		return PaginationParams{}
	}

	page := 1
	if parsed, err := strconv.Atoi(pageRaw); err == nil && parsed > 0 {
		page = parsed
	}
	// Bound the page number. Without this, (page-1)*perPage overflows int64 for
	// a large enough page and wraps negative, which used to make PaginateSlice
	// index a slice with a negative bound and panic. maxPage is far past any
	// real data set, so clamping is indistinguishable from "past the end".
	if page > maxPage {
		page = maxPage
	}

	perPage := 50
	if parsed, err := strconv.Atoi(perPageRaw); err == nil && parsed > 0 {
		perPage = parsed
	}
	if perPage > maxPerPage {
		perPage = maxPerPage
	}

	return PaginationParams{
		Page:    page,
		PerPage: perPage,
		Enabled: true,
	}
}

func PaginateSlice[T any](items []T, page PaginationParams) ([]T, int) {
	total := len(items)
	if !page.Enabled {
		return items, total
	}
	// Defensive even though ParsePagination clamps: PaginationParams is an
	// exported struct, so a caller can build one by hand and reach this with
	// values that never passed through the parser.
	if page.Page < 1 || page.PerPage < 1 {
		return []T{}, total
	}
	// Reject pages beyond the data *before* computing the offset, rather than
	// computing the offset and sanity-checking it afterwards. Checking after is
	// not sufficient: (page-1)*perPage can overflow to a positive number or to
	// exactly zero (1<<62 * 500 wraps to 0), which silently returns the first
	// page instead of an empty one. Comparing against the data first means the
	// multiplication below is bounded by total and cannot overflow at all.
	if page.Page > total/page.PerPage+1 {
		return []T{}, total
	}
	start := (page.Page - 1) * page.PerPage
	if start >= total {
		return []T{}, total
	}
	end := start + page.PerPage
	if end > total {
		end = total
	}
	return items[start:end], total
}
