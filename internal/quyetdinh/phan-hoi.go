package quyetdinh

import (
	"encoding/json"
	"net/http"
	"time"
)

// PhanHoi là phản hồi do gate tự trả (302/401/402/503/508). MỌI phản hồi của gate đều `Cache-Control: no-store`: kết quả phụ thuộc
// người gọi và thời điểm; để CDN/trình duyệt đệm là cho người khác xem nhầm hoặc kẹt ở màn chặn sau khi đã trả tiền.
type PhanHoi struct {
	Status int
	Header http.Header
	Than   []byte
}

// Ghi gửi phản hồi.
func (p *PhanHoi) Ghi(w http.ResponseWriter) {
	for k, v := range p.Header {
		w.Header()[k] = v
	}
	w.WriteHeader(p.Status)
	if len(p.Than) > 0 {
		_, _ = w.Write(p.Than)
	}
}

// Mã `code` trong thân JSON của gate (hợp đồng với client HTTP của app, giống SDK).
const (
	MaKhongDanhTinh = "dz_no_identity"
	MaKhongCoQuyen  = "dz_not_entitled"
	MaHubKhongSan   = "dz_hub_unavailable"
	MaDanhTinhKhong = "dz_identity_unavailable"
	MaVong          = "dz_gate_loop"
	MaUpstream      = "dz_upstream_unavailable"
	MaKhongCo       = "dz_not_found"
)

// RetryAfterDanhTinh: giá trị `Retry-After` (giây) của 503 `dz_identity_unavailable`.
const RetryAfterDanhTinh = "5"

// DinhDangMoc: RFC 3339 UTC, bỏ phần thập phân thừa (`…:00Z`) — khớp `formatInstant` của SDK.
func DinhDangMoc(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// phanHoiJSON: HEAD không có thân; còn lại JSON UTF-8.
func phanHoiJSON(phuongThuc string, status int, than any) *PhanHoi {
	h := http.Header{}
	h.Set("Cache-Control", "no-store")
	h.Set("Content-Type", "application/json; charset=utf-8")
	p := &PhanHoi{Status: status, Header: h}
	if phuongThuc != http.MethodHead {
		p.Than, _ = json.Marshal(than)
	}
	return p
}

func chuyenHuong(location string) *PhanHoi {
	h := http.Header{}
	h.Set("Location", location)
	h.Set("Cache-Control", "no-store")
	return &PhanHoi{Status: http.StatusFound, Header: h}
}

type thanLoi struct {
	Code   string `json:"code"`
	Status int    `json:"status"`
}

func phanHoiLoi(phuongThuc string, status int, ma string) *PhanHoi {
	return phanHoiJSON(phuongThuc, status, thanLoi{Code: ma, Status: status})
}

type than402 struct {
	Code        string  `json:"code"`
	Status      string  `json:"status"` // trạng thái thuê bao (không phải mã HTTP)
	PlanCode    string  `json:"plan_code"`
	CheckoutURL string  `json:"checkout_url,omitempty"` // VẮNG (không phải null) khi không tạo được phiên
	GraceUntil  *string `json:"grace_until"`
}

func phanHoi402(phuongThuc string, q Quyet, maGoi string) *PhanHoi {
	b := than402{Code: MaKhongCoQuyen, Status: string(q.KetQua.TrangThai), PlanCode: maGoi, CheckoutURL: q.CheckoutURL}
	if g := q.KetQua.HetAnHan; g != nil {
		s := DinhDangMoc(*g)
		b.GraceUntil = &s
	}
	return phanHoiJSON(phuongThuc, http.StatusPaymentRequired, b)
}

// PhanHoiKhongTimThay: 404 JSON `dz_not_found`, `no-store` — vùng `/_dz/*` thuộc về gate, không chuyển tiếp tới app.
func PhanHoiKhongTimThay(phuongThuc string) *PhanHoi {
	return phanHoiLoi(phuongThuc, http.StatusNotFound, MaKhongCo)
}

// PhanHoiUpstreamLoi: upstream (app của dev) không trả lời ⇒ 502 JSON `dz_upstream_unavailable`, `no-store`. KHÔNG kèm chi tiết lỗi mạng
// (có thể lộ địa chỉ nội bộ).
func PhanHoiUpstreamLoi(phuongThuc string) *PhanHoi {
	return phanHoiLoi(phuongThuc, http.StatusBadGateway, MaUpstream)
}

// Mã lỗi riêng của chế độ forward-auth (`GET /_dz/auth`).
const (
	MaForwardKhongTin = "dz_forward_untrusted"
	MaForwardSai      = "dz_forward_bad_request"
)

// LyDo của phản hồi forward-auth do gate tự chặn trước khi vào lõi quyết định (nhãn log/metric hữu hạn).
const (
	LyDoForwardKhongTin = "forward_untrusted"
	LyDoForwardSai      = "forward_bad_request"
)

// PhanHoiForwardKhongTin: 403 — kết nối không thuộc gate.forward_auth.allow_from. Không gọi hub.
func PhanHoiForwardKhongTin(phuongThuc string) *PhanHoi {
	return phanHoiLoi(phuongThuc, http.StatusForbidden, MaForwardKhongTin)
}

// PhanHoiForwardSai: 400 — thiếu/hỏng X-Forwarded-Uri|X-Original-URI hoặc Host không khớp paywall.public_origins.
func PhanHoiForwardSai(phuongThuc string) *PhanHoi {
	return phanHoiLoi(phuongThuc, http.StatusBadRequest, MaForwardSai)
}
