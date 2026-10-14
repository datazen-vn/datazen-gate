package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/datazen-vn/market-gate/internal/danhtinh"
)

var (
	maGoiRe        = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)
	urlQuayVeHTTPS = regexp.MustCompile(`^https://[A-Za-z0-9.-]+(:[0-9]{1,5})?(/[\x21-\x7e]*)?$`)
	urlQuayVeLoop  = regexp.MustCompile(`^http://(localhost|127\.0\.0\.1):[0-9]{1,5}(/[\x21-\x7e]*)?$`)
)

const toiDaDoDaiURL = 1000

// TaoPhienYeuCau: đầu vào `POST /v1/checkout-sessions` của cổng. success_url/cancel_url do cổng dựng từ public_origins + đường dẫn cố
// định (không bao giờ có `?` hay `#` — hub từ chối và tự ghép `?checkout_session_id=<uuid>`).
type TaoPhienYeuCau struct {
	MaKhach    string
	MaGoi      string
	SuccessURL string
	CancelURL  string
}

// Phien là phiên thanh toán hosted vừa tạo/trả lại.
type Phien struct {
	ID     string
	URL    string
	HetHan time.Time
	// DoiSau: mốc hub bắt đầu THAY phiên (= HetHan − 10 phút). Đệm URL KHÔNG được quá mốc này. nil = hub (cũ) không báo.
	DoiSau *time.Time
}

// XemPhien là phiên đọc lại bằng DocPhien (`GET /v1/checkout-sessions/{id}`); URL chỉ có khi TrangThai = "open".
type XemPhien struct {
	ID        string
	TrangThai string
	URL       string
	HetHan    time.Time
}

// cheDoTest: dz_test_ ⇒ "test" (nhận thêm http://localhost:<cổng>); khác ⇒ "live".
func (k *Khach) cheDoTest() bool { return strings.HasPrefix(k.cfg.APIKey, "dz_test_") }

func (k *Khach) kiemURLQuayVe(raw, ten string) error {
	if raw == "" {
		return nil
	}
	dai := len(raw)
	https := urlQuayVeHTTPS.MatchString(raw)
	loop := urlQuayVeLoop.MatchString(raw)
	switch {
	case dai > toiDaDoDaiURL || (!https && !loop):
		return &Loi{Ma: LoiCauHinh, ChiTiet: ten + " phải là https://host[:cổng][/đường-dẫn] (hoặc http://localhost:<cổng> với khoá dz_test_), ≤ 1000 ký tự"}
	case loop && !k.cheDoTest():
		return &Loi{Ma: LoiCauHinh, ChiTiet: ten + " dùng http://localhost chỉ hợp lệ với khoá dz_test_; khoá live cần https://"}
	}
	return nil
}

func anhXaCheckout(l *Loi) *Loi { l.Ma = LoiCheckout; return l }

// TaoPhien gọi `POST /v1/checkout-sessions`. Hub tự trả lại phiên `open` cũ cho cùng (khách, gói) (200; phiên mới là 201) nên gọi lại
// không tạo đơn đôi. Lỗi 4xx (ngoài 401/429) ⇒ dz_checkout_failed với HubCode; 409 `phien-da-nhan-tien` mang PhienLienQuan.
func (k *Khach) TaoPhien(ctx context.Context, y TaoPhienYeuCau) (*Phien, error) {
	if !danhtinh.MaKhachHopLe(y.MaKhach) {
		return nil, loi(LoiMaKhach, "customer_ref sai hình dạng")
	}
	if !maGoiRe.MatchString(y.MaGoi) {
		return nil, loi(LoiCauHinh, "plan_code phải khớp ^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$")
	}
	if err := k.kiemURLQuayVe(y.SuccessURL, "success_url"); err != nil {
		return nil, err
	}
	if err := k.kiemURLQuayVe(y.CancelURL, "cancel_url"); err != nil {
		return nil, err
	}
	than := map[string]string{"customer_ref": y.MaKhach, "plan_code": y.MaGoi}
	if y.SuccessURL != "" {
		than["success_url"] = y.SuccessURL
	}
	if y.CancelURL != "" {
		than["cancel_url"] = y.CancelURL
	}
	b, err := json.Marshal(than)
	if err != nil {
		return nil, &Loi{Ma: LoiCauHinh, ChiTiet: "không mã hoá được thân", Goc: err}
	}
	res, err := k.gui(ctx, yeuCau{
		phuongThuc: http.MethodPost, duongDan: "/v1/checkout-sessions", duongDanGiai: "/v1/checkout-sessions",
		than: b, anhXaLoi4xx: anhXaCheckout,
	})
	if err != nil {
		return nil, err
	}
	return k.docPhien(res.than)
}

// DocPhien gọi `GET /v1/checkout-sessions/{id}`; `id` phải là uuid (đi vào đường dẫn: chặn `../`, `?`).
func (k *Khach) DocPhien(ctx context.Context, id string) (*XemPhien, error) {
	if !uuidRe.MatchString(id) {
		return nil, loi(LoiCauHinh, "id phiên phải là uuid")
	}
	res, err := k.gui(ctx, yeuCau{
		phuongThuc: http.MethodGet, duongDan: "/v1/checkout-sessions/" + id, duongDanGiai: "/v1/checkout-sessions/" + id,
		anhXaLoi4xx: anhXaCheckout,
	})
	if err != nil {
		return nil, err
	}
	var w struct {
		ID      string  `json:"id"`
		Status  string  `json:"status"`
		URL     *string `json:"url"`
		Expires *string `json:"expires_at"`
	}
	if json.Unmarshal(res.than, &w) != nil || w.ID == "" || len(w.ID) > 256 || w.Status == "" {
		return nil, loi(LoiPhanHoi, "thiếu hoặc sai `id`/`status`")
	}
	het, err := docMoc(w.Expires, "expires_at")
	if err != nil || het == nil {
		return nil, loi(LoiPhanHoi, "`expires_at` không phải mốc thời gian ISO-8601")
	}
	x := &XemPhien{ID: w.ID, TrangThai: w.Status, HetHan: *het}
	if w.URL != nil {
		u, err := PhanTichURLAnToan(*w.URL, "url")
		if err != nil {
			return nil, loi(LoiPhanHoi, "`url` không phải URL https hợp lệ")
		}
		x.URL = u.String()
	}
	return x, nil
}

// docPhien: CHẶT ở thứ cổng dùng để chuyển hướng khách (`url` https, hoặc http localhost khi chạy hub dev), `id`, `expires_at`.
func (k *Khach) docPhien(than []byte) (*Phien, error) {
	var w struct {
		ID           string  `json:"id"`
		URL          string  `json:"url"`
		ExpiresAt    *string `json:"expires_at"`
		ReplaceAfter *string `json:"replace_after"`
	}
	if json.Unmarshal(than, &w) != nil {
		return nil, loi(LoiPhanHoi, "thân không phải đối tượng JSON")
	}
	if w.ID == "" || len(w.ID) > 256 {
		return nil, loi(LoiPhanHoi, "thiếu hoặc sai trường `id`")
	}
	u, err := PhanTichURLAnToan(w.URL, "url")
	if err != nil {
		return nil, loi(LoiPhanHoi, "`url` thiếu hoặc không phải URL https hợp lệ")
	}
	het, err := docMoc(w.ExpiresAt, "expires_at")
	if err != nil || het == nil {
		return nil, loi(LoiPhanHoi, "`expires_at` không phải mốc thời gian ISO-8601")
	}
	// `replace_after` tuỳ chọn (hub cũ không có); có mà sai hình ⇒ sai hợp đồng, không đoán.
	doi, err := docMoc(w.ReplaceAfter, "replace_after")
	if err != nil {
		return nil, err
	}
	return &Phien{ID: w.ID, URL: u.String(), HetHan: *het, DoiSau: doi}, nil
}
