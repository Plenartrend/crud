package main

import (
	"net/http"
	"strconv"
)

// PaginationMeta holds pagination metadata for building responses.
// Backend sends total_items; frontend sends page_size. Page is 1-based.
type PaginationMeta struct {
	Page       int
	PageSize   int
	TotalItems int
}

// PaginatedResponse allows shared code (e.g. logging, middleware) to work with any paginated response.
type PaginatedResponse interface {
	GetPage() int
	GetPageSize() int
	GetTotalItems() int
}

func (p PaginationMeta) GetPage() int       { return p.Page }
func (p PaginationMeta) GetPageSize() int   { return p.PageSize }
func (p PaginationMeta) GetTotalItems() int { return p.TotalItems }

// DefaultPageSize is the default number of items per page when the client omits page_size.
const DefaultPageSize = 20

// MaxPageSize is the maximum allowed page_size.
const MaxPageSize = 100

// NormalizePagination applies defaults and clamps page and page_size.
// Use when reading query params (page from client, page_size from client).
func NormalizePagination(page, pageSize int) (p, size int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}
	return page, pageSize
}

// PaginationFromRequest extracts page_size and offset from the request query.
// Frontend sends page_size and offset; no need for page. Returns pageSize and offset for SQL (LIMIT, OFFSET).
// Invalid or omitted values use defaults (page_size 20, offset 0).
func PaginationFromRequest(r *http.Request) (pageSize, offset int) {
	q := r.URL.Query()
	ps := DefaultPageSize
	if v := q.Get("page_size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			ps = n
		}
	}
	off := 0
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			off = n
		}
	}
	if ps < 1 {
		ps = DefaultPageSize
	}
	if ps > MaxPageSize {
		ps = MaxPageSize
	}
	return ps, off
}

// PageFromOffset returns the 1-based page number for a given offset and page size.
// Use when building the response so the client can display "Page X of Y" without recomputing.
func PageFromOffset(offset, pageSize int) int {
	if pageSize <= 0 {
		return 1
	}
	return offset/pageSize + 1
}
