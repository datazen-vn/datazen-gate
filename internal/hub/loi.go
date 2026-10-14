// Package hub là client của hub DataZen cho dz-gate: đọc quyền dùng (`GET /v1/customers/{ref}/entitlements`, ETag/304) với bộ đệm,
// stale-if-error, gộp lời gọi và trần đọc tươi; tạo/đọc phiên thanh toán (`/v1/checkout-sessions`).
//
// Luật cứng (conformance gate-v1): FAIL-CLOSED mặc định; chỉ lỗi mạng/timeout/5xx được stale-if-error hay fail_mode open cứu —
// 401/403/404/429 là lỗi khoá/cấu hình/hạn mức ⇒ không bao giờ được cứu. Không theo chuyển hướng (3xx ⇒ lỗi cấu hình).
package hub

import (
	"errors"
	"fmt"
)

// Mã lỗi ổn định (cùng tên với `DataZenError.code` của SDK để tài liệu/log dùng chung).
const (
	LoiMang        = "dz_network_error"
	LoiHetGio      = "dz_timeout"
	LoiMayChu      = "dz_server_error"
	LoiHanMuc      = "dz_rate_limited"
	LoiKhoa        = "dz_service_key_invalid"
	LoiPhanHoi     = "dz_bad_response"
	LoiChuyenHuong = "dz_redirect_refused"
	LoiYeuCau      = "dz_bad_request"
	LoiTrangThai   = "dz_unexpected_status"
	LoiCheckout    = "dz_checkout_failed"
	LoiCauHinh     = "dz_config_invalid"
	LoiMaKhach     = "dz_invalid_customer_ref"
)

// Loi là lỗi có mã của client hub. Không bao giờ chứa apiKey, cookie hay customer_ref.
type Loi struct {
	Ma      string
	HTTP    int    // 0 khi chưa có phản hồi
	HubCode string // `Error.code` của hub (kebab-case), nếu có
	// PhienLienQuan: `details.checkout_session_id` của 409 phien-da-nhan-tien (chỉ uuid).
	PhienLienQuan string
	ChiTiet       string
	Goc           error
}

func (l *Loi) Error() string {
	s := l.Ma
	if l.ChiTiet != "" {
		s += ": " + l.ChiTiet
	}
	if l.HubCode != "" {
		s += fmt.Sprintf(" [hub_code=%s]", l.HubCode)
	}
	return s
}

func (l *Loi) Unwrap() error { return l.Goc }

// HaTang: sự cố hạ tầng (mạng/timeout/5xx) — loại DUY NHẤT được stale-if-error và fail_mode open cứu. 429 là hạn mức (cho cứu thì
// kẻ tấn công gây quá tải hub để mọi tuyến fail-open thành miễn phí).
func (l *Loi) HaTang() bool {
	return l.Ma == LoiMang || l.Ma == LoiHetGio || l.Ma == LoiMayChu
}

// Thu lai được: HaTang hoặc 429.
func (l *Loi) ThuLaiDuoc() bool { return l.HaTang() || l.Ma == LoiHanMuc }

func loi(ma, chiTiet string) *Loi { return &Loi{Ma: ma, ChiTiet: chiTiet} }

// ALoi trả *Loi nếu err (hoặc chuỗi bọc của nó) là Loi.
func ALoi(err error) (*Loi, bool) {
	var l *Loi
	if errors.As(err, &l) {
		return l, true
	}
	return nil, false
}
