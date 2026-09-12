package utils

import (
	"net/http"
	"strconv"
	"strings"
)

// Pagination holds the page a client asked for.
type Pagination struct {

	// Page is the 1-based page number.
	Page int `json:"page"`

	// PerPage is how many rows fit on one page.
	PerPage int `json:"per_page"`
}

// Offset converts the page number into the number of
// rows the database should skip.
//
// Page 1 skips nothing, page 2 skips one page worth
// of rows, and so on.
func (p Pagination) Offset() int {

	return (p.Page - 1) * p.PerPage
}

// Constants that keep pagination predictable.
//
// A client can never ask for more than maxPerPage rows
// at a time, which stops one request from pulling the
// whole catalog into memory.
const (
	defaultPerPage = 20
	maxPerPage     = 100
)

// ParsePagination reads the page and per_page query
// parameters.
//
// Missing or unreadable values fall back to sensible
// defaults instead of failing the request, because a
// client that forgets to send a page number still
// deserves an answer.
func ParsePagination(r *http.Request) Pagination {

	query := r.URL.Query()

	page := parsePositiveInt(
		query.Get("page"),
		1,
	)

	perPage := parsePositiveInt(
		query.Get("per_page"),
		defaultPerPage,
	)

	// Never allow more than the maximum.
	if perPage > maxPerPage {
		perPage = maxPerPage
	}

	return Pagination{
		Page:    page,
		PerPage: perPage,
	}
}

// parsePositiveInt converts a string to a positive
// whole number, falling back to a default when the
// text is missing or not a usable number.
func parsePositiveInt(
	value string,
	fallback int,
) int {

	value = strings.TrimSpace(value)

	if value == "" {
		return fallback
	}

	number, err := strconv.Atoi(value)

	if err != nil || number < 1 {
		return fallback
	}

	return number
}

// Page is the envelope returned by every paginated
// endpoint.
//
// Keeping the shape identical across products, orders,
// and reviews means a client only has to learn it once.
type Page[T any] struct {

	// Data holds the rows for this page.
	Data []T `json:"data"`

	// Page is the page number that was returned.
	Page int `json:"page"`

	// PerPage is how many rows fit on one page.
	PerPage int `json:"per_page"`

	// Total is how many rows matched overall.
	Total int `json:"total"`

	// TotalPages is how many pages those rows fill.
	TotalPages int `json:"total_pages"`
}

// NewPage builds a pagination envelope.
//
// TotalPages is rounded up, so three rows over two
// per page gives two pages, not one.
func NewPage[T any](
	data []T,
	pagination Pagination,
	total int,
) Page[T] {

	// An empty result still has one page, so that
	// total_pages is never zero and clients can
	// always display "page 1 of 1".
	totalPages := 1

	if total > 0 {

		totalPages = (total + pagination.PerPage - 1) /
			pagination.PerPage
	}

	return Page[T]{
		Data: data,

		Page: pagination.Page,

		PerPage: pagination.PerPage,

		Total: total,

		TotalPages: totalPages,
	}
}
