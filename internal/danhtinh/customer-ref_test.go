package danhtinh

import (
	"strings"
	"testing"
)

func TestMaKhachHopLe(t *testing.T) {
	cacTruongHop := []struct {
		ma   string
		mong bool
	}{
		{"cus_1", true},
		{"a", true},
		{strings.Repeat("a", 200), true},
		{strings.Repeat("a", 201), false},
		{"", false},
		{".", false},
		{"..", false},
		{"..a", true}, // sát biên nhưng hợp lệ
		{"a.b", true},
		{"a/b", true},
		{"a?b#c%", true},
		{"%2e%2e", true},
		{"user@example.com", true},
		{"co khoang trang", false},
		{"tab\there", false},
		{"xuong\ndong", false},
		{"nul\x00", false},
		{"tiếng-việt", false},
		{"\x7f", false},
	}
	for _, c := range cacTruongHop {
		if got := MaKhachHopLe(c.ma); got != c.mong {
			t.Errorf("MaKhachHopLe(%q) = %v, mong %v", c.ma, got, c.mong)
		}
	}
}
