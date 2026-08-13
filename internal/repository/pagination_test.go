package repository

import "testing"

func TestNormalizePageBounds(t *testing.T) {
	for _, tc := range []struct {
		page, size, wantPage, wantSize int
	}{
		{1, 20, 1, 20},
		{0, 0, 1, 20},
		{-2, -1, 1, 20},
		{3, 101, 3, 20},
		{4, 100, 4, 100},
	} {
		page, size := normalizePageBounds(tc.page, tc.size)
		if page != tc.wantPage || size != tc.wantSize {
			t.Errorf("normalizePageBounds(%d,%d)=(%d,%d), want (%d,%d)", tc.page, tc.size, page, size, tc.wantPage, tc.wantSize)
		}
	}
}
