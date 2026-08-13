package repository

const (
	defaultPageSize = 20
	maxPageSize     = 100
)

// normalizePageBounds keeps every user-facing list query within a bounded
// database/response budget. A caller may request a smaller page, but never an
// unbounded result set through a negative or oversized size value.
func normalizePageBounds(page, size int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if size <= 0 || size > maxPageSize {
		size = defaultPageSize
	}
	return page, size
}
