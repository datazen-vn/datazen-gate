package quyetdinh

import (
	"context"
	"errors"
	"net/http"

	"github.com/datazen-vn/datazen-gate/internal/hub"
)

type ketQuaTuChoi struct {
	ketQua  KetQua
	xungDot bool // 409 thue-bao-dang-hieu-luc: bản đệm quyền của ta cũ, người gọi nên hỏi lại hub
}

// tuChoi dựng phản hồi cho một quyết định CHẶN. not_entitled: tạo/lấy phiên thanh toán; điều hướng trình duyệt ⇒ 302 tới trang
// thanh toán, còn API/XHR/non-GET ⇒ 402 JSON (không bao giờ 302 cho non-GET). Tạo phiên lỗi ⇒ vẫn 402, chỉ thiếu checkout_url.
func (c *Cong) tuChoi(ctx context.Context, r *http.Request, q Quyet, route Route) ketQuaTuChoi {
	dieuHuong := LaDieuHuong(r.Method, r.Header)
	var p *PhanHoi
	xungDot := false
	switch q.LyDo {
	case LyDoKhongDanhTinh:
		if dieuHuong && c.Paywall.LoginURL != "" {
			p = chuyenHuong(c.Paywall.LoginURL)
		} else {
			p = phanHoiLoi(r.Method, q.Status, MaKhongDanhTinh)
		}
	case LyDoKhongSan:
		ma := MaHubKhongSan
		if q.Tu == TuDanhTinh {
			ma = MaDanhTinhKhong
		}
		p = phanHoiLoi(r.Method, q.Status, ma)
		if q.Tu == TuDanhTinh {
			p.Header.Set("Retry-After", RetryAfterDanhTinh) // lỗi tạm; cố định 5 giây (README gate-v1)
		}
	case LyDoKhongCoQuyen:
		goi := route.MaGoi
		if goi == "" {
			goi = c.Paywall.MaGoi
		}
		// Khách ĐÃ active đúng gói này mà vẫn thiếu feature của tuyến: không có gì để bán thêm cùng gói (hub sẽ 409 thue-bao-dang-hieu-luc)
		// ⇒ 402 không checkout_url, KHÔNG tạo phiên (panel an ninh C1). Gói khác (đổi gói) vẫn tạo phiên.
		if q.KetQua.TrangThai == hub.Active && q.KetQua.MaGoi == goi {
			return ketQuaTuChoi{ketQua: KetQua{Quyet: q, PhanHoi: phanHoi402(r.Method, q, goi)}}
		}
		url, err := c.Paywall.URLThanhToan(ctx, q.MaKhach, goi, c.Paywall.URLQuayVe(r.Host))
		if err != nil {
			// Khách vẫn không có quyền ⇒ vẫn 402, chỉ thiếu đường dẫn thanh toán; lỗi đi vào BaoLoi.
			var l *hub.Loi
			xungDot = errors.As(err, &l) && l.HubCode == HubCodeThueBaoHieuLuc
			c.baoLoi(err)
		} else {
			q.CheckoutURL = url
		}
		if dieuHuong && q.CheckoutURL != "" {
			p = chuyenHuong(q.CheckoutURL)
		} else {
			p = phanHoi402(r.Method, q, goi)
		}
	default: // LyDoVong: đã xử lý ở XuLy
		p = phanHoiLoi(r.Method, q.Status, MaVong)
	}
	return ketQuaTuChoi{ketQua: KetQua{Quyet: q, PhanHoi: p}, xungDot: xungDot}
}
