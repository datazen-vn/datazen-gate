package quyetdinh

import (
	"net/http"
	"testing"
)

func TestLaDieuHuong(t *testing.T) {
	cacTruongHop := []struct {
		ten, pt string
		h       map[string]string
		mong    bool
	}{
		{"navigate", "GET", map[string]string{"Sec-Fetch-Mode": "navigate"}, true},
		{"HEAD navigate", "HEAD", map[string]string{"Sec-Fetch-Mode": "navigate"}, true},
		{"navigate hoa/khoảng trắng", "GET", map[string]string{"Sec-Fetch-Mode": " Navigate "}, true},
		{"cors dù Accept html", "GET", map[string]string{"Sec-Fetch-Mode": "cors", "Accept": "text/html"}, false},
		{"same-origin", "GET", map[string]string{"Sec-Fetch-Mode": "same-origin", "Accept": "text/html"}, false},
		{"POST navigate", "POST", map[string]string{"Sec-Fetch-Mode": "navigate"}, false},
		{"X-Requested-With", "GET", map[string]string{"Sec-Fetch-Mode": "navigate", "X-Requested-With": "XMLHttpRequest"}, false},
		{"không Sec-Fetch, Accept html", "GET", map[string]string{"Accept": "text/html,application/xhtml+xml"}, true},
		{"không Sec-Fetch, Accept json", "GET", map[string]string{"Accept": "application/json"}, false},
		{"không Sec-Fetch, Accept */*", "GET", map[string]string{"Accept": "*/*"}, false},
		{"không Accept", "GET", nil, false},
		{"html trước json cùng q", "GET", map[string]string{"Accept": "text/html, application/json"}, true},
		{"json trước html cùng q", "GET", map[string]string{"Accept": "application/json, text/html"}, false},
		{"json q cao hơn", "GET", map[string]string{"Accept": "text/html;q=0.5, application/json;q=0.9"}, false},
		{"html q cao hơn", "GET", map[string]string{"Accept": "application/json;q=0.5, text/html;q=0.9"}, true},
		{"html q=0", "GET", map[string]string{"Accept": "text/html;q=0"}, false},
		{"q hỏng bị bỏ (mặc định 1)", "GET", map[string]string{"Accept": "text/html;q=-1, application/json;q=0.5"}, true},
		{"+json cũng là json", "GET", map[string]string{"Accept": "application/problem+json, text/html"}, false},
	}
	for _, c := range cacTruongHop {
		h := http.Header{}
		for k, v := range c.h {
			h.Set(k, v)
		}
		if got := LaDieuHuong(c.pt, h); got != c.mong {
			t.Errorf("%s: %v, mong %v", c.ten, got, c.mong)
		}
	}
}

func TestLaQuayVe(t *testing.T) {
	const id = "5b0c2f3e-8d41-4a6b-9c7e-2f1a3b4c5d6e"
	cacTruongHop := []struct {
		pt, q string
		mong  bool
	}{
		{"GET", "checkout_session_id=" + id, true},
		{"HEAD", "checkout_session_id=" + id, true},
		{"POST", "checkout_session_id=" + id, false},
		{"GET", "a=1&checkout_session_id=" + id + "&b=2", true},
		{"GET", "checkout_session_id=5B0C2F3E-8D41-4A6B-9C7E-2F1A3B4C5D6E", true},
		{"GET", "checkout_session_id=" + id + "&checkout_session_id=rac", true},
		{"GET", "checkout_session_id=rac&checkout_session_id=" + id, false}, // chỉ giá trị ĐẦU TIÊN
		{"GET", "checkout_session_id=cs_3fK9aB0cD1eF2gH3iJ4kL5", false},
		{"GET", "checkout_session_id=" + id + "x", false},
		{"GET", "checkout_session_id=" + id + "%0A", false},
		{"GET", "checkout_session_id=%2e%2e%2fadmin", false},
		{"GET", "checkout_session_id=", false},
		{"GET", "", false},
		{"GET", "checkout_session_ids=" + id, false},
		{"GET", "%zz&checkout_session_id=" + id, true}, // phần hỏng không làm mất phần lành
	}
	for _, c := range cacTruongHop {
		if got := LaQuayVe(c.pt, c.q); got != c.mong {
			t.Errorf("%s ?%s: %v, mong %v", c.pt, c.q, got, c.mong)
		}
	}
}
