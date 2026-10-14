package quyetdinh

import (
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// thamSoQuayVe: hub nối `?checkout_session_id=<uuid>` vào `success_url` khi đưa khách quay về (không PII).
const thamSoQuayVe = "checkout_session_id"

// Id phiên checkout là uuid (không phân biệt hoa/thường). `cs_…` là mã capability của trang hosted, KHÔNG bao giờ nằm ở đây.
// soQRe: giá trị q hợp lệ chỉ gồm chữ số và dấu chấm (khác ⇒ bỏ tham số, q giữ mặc định 1 — khớp SDK).
var soQRe = regexp.MustCompile(`^[0-9.]+$`)

var uuidRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// LaQuayVe: khách vừa QUAY VỀ từ trang thanh toán? Chỉ GET/HEAD có `checkout_session_id` (giá trị ĐẦU TIÊN) đúng khuôn uuid. Khi đó
// quyền được đọc tươi từ hub, không dùng bản đệm "chưa có quyền". Hình sai (`../x`, `cs_…`) ⇒ coi như không có.
func LaQuayVe(phuongThuc, rawQuery string) bool {
	if phuongThuc != http.MethodGet && phuongThuc != http.MethodHead {
		return false
	}
	q, _ := url.ParseQuery(rawQuery) // phần hỏng bị bỏ, phần lành vẫn đọc được
	return uuidRe.MatchString(q.Get(thamSoQuayVe))
}

// LaDieuHuong: điều hướng của trình duyệt (⇒ 302) hay lời gọi API/XHR (⇒ 402 JSON)? Chỉ GET/HEAD, không có `X-Requested-With`.
// `Sec-Fetch-Mode` (trình duyệt đặt, JS không giả được) nếu có thì quyết định: `navigate` ⇒ điều hướng, mode khác ⇒ fetch/XHR dù
// `Accept` ghi text/html. Không có (curl, trình duyệt cũ) ⇒ xét `Accept` có ưu tiên `text/html` hơn JSON không.
func LaDieuHuong(phuongThuc string, h http.Header) bool {
	if phuongThuc != http.MethodGet && phuongThuc != http.MethodHead {
		return false
	}
	if len(h.Values("X-Requested-With")) > 0 {
		return false
	}
	if mode := h.Values("Sec-Fetch-Mode"); len(mode) > 0 {
		return strings.ToLower(strings.TrimSpace(strings.Join(mode, ", "))) == "navigate"
	}
	return acceptUuTienHTML(strings.Join(h.Values("Accept"), ","), len(h.Values("Accept")) > 0)
}

type diemAccept struct {
	q  float64
	vi int
}

func acceptUuTienHTML(accept string, co bool) bool {
	if !co {
		return false
	}
	var html, json *diemAccept
	for vi, phan := range strings.Split(accept, ",") {
		cacTham := strings.Split(strings.ToLower(strings.TrimSpace(phan)), ";")
		kieu := cacTham[0]
		q := 1.0
		for _, tham := range cacTham[1:] {
			tham = strings.TrimSpace(tham)
			if v, ok := strings.CutPrefix(tham, "q"); ok {
				if v, ok = strings.CutPrefix(strings.TrimSpace(v), "="); ok && soQRe.MatchString(strings.TrimSpace(v)) {
					f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
					if err != nil {
						f = 0
					}
					q = f
				}
			}
		}
		d := &diemAccept{q, vi}
		switch {
		case kieu == "text/html" || kieu == "application/xhtml+xml":
			if html == nil || q > html.q {
				html = d
			}
		case kieu == "application/json" || strings.HasSuffix(kieu, "+json"):
			if json == nil || q > json.q {
				json = d
			}
		}
	}
	if html == nil || html.q <= 0 {
		return false
	}
	if json == nil {
		return true
	}
	// Cùng q thì loại nào đứng trước trong Accept được ưu tiên.
	return html.q > json.q || (html.q == json.q && html.vi < json.vi)
}
