package quyetdinh

import (
	"context"
	"errors"
	"time"

	"github.com/datazen-vn/market-gate/internal/hub"
)

func khoaPhien(ma, goi string) string { return ma + "\x00" + goi }

// dungDen: mốc ngừng dùng URL phiên đã đệm — `replace_after` − lệch đồng hồ; hub cũ không báo ⇒ expires_at − 11 phút. Đệm quá mốc này
// đẩy khách vào QR chỉ còn vài phút, hoặc (nhiều instance) vào trang "đã thay — đừng chuyển" khi instance khác đã khiến hub thay phiên.
func dungDen(p *hub.Phien) time.Time {
	if p.DoiSau != nil {
		return p.DoiSau.Add(-lechDongHo)
	}
	return p.HetHan.Add(-truocHetHanDuPhong)
}

// URLThanhToan trả URL trang thanh toán của (khách, gói): từ đệm, hoặc tạo/lấy lại phiên ở hub (cùng khách bấm nhiều lần không sinh
// phiên mới; gọi đồng thời gộp thành một). Tạo lỗi ⇒ nhớ lỗi 5 giây rồi trả lỗi. 409 phien-da-nhan-tien ⇒ đọc lại phiên cũ và đưa
// khách về đúng phiên đó nếu còn `open`.
func (p *Paywall) URLThanhToan(ctx context.Context, ma, goi string, u URLQuayVe) (string, error) {
	khoa := khoaPhien(ma, goi)
	if m, ok := p.phien.Lay(khoa); ok && p.phien.Tuoi(m) {
		return m.GiaTri, nil
	}
	if m, ok := p.loi.Lay(khoa); ok && p.loi.Tuoi(m) {
		return "", m.GiaTri
	}
	p.mu.Lock()
	p.cacGoi[goi] = true
	p.mu.Unlock()
	url, err, _ := p.nhom.Chay(khoa, func() (string, error) {
		s, err := p.co.TaoPhien(ctx, hub.TaoPhienYeuCau{MaKhach: ma, MaGoi: goi, SuccessURL: u.Success, CancelURL: u.Cancel})
		if err != nil {
			if cu := p.urlPhienCu(ctx, err); cu != "" {
				p.phien.Dat(khoa, cu, p.now().Add(nhoLoi))
				return cu, nil
			}
			p.loi.Dat(khoa, err, p.now().Add(nhoLoi))
			return "", err
		}
		if den := dungDen(s); den.After(p.now()) {
			p.phien.Dat(khoa, s.URL, den)
		}
		return s.URL, nil
	})
	return url, err
}

// urlPhienCu: URL phiên cũ khi `err` là 409 phien-da-nhan-tien và phiên còn `open`; mọi trường hợp khác (kể cả đọc lại lỗi) ⇒ "",
// giữ lỗi gốc. Không bao giờ đưa khách về phiên đã trả dù hub lỡ kèm url.
func (p *Paywall) urlPhienCu(ctx context.Context, err error) string {
	var l *hub.Loi
	if !errors.As(err, &l) || l.HubCode != HubCodePhienDaNhanTien || l.PhienLienQuan == "" {
		return ""
	}
	cu, err := p.co.DocPhien(ctx, l.PhienLienQuan)
	if err != nil || cu.TrangThai != "open" {
		return ""
	}
	return cu.URL
}

// Quen bỏ phiên đã đệm và nhớ lỗi của khách (mọi gói đã biết): khách đã trả rồi huỷ không bị đưa về phiên cũ; hết hạn thuê bao thì xin
// phiên MỚI.
func (p *Paywall) Quen(ma string) {
	p.mu.Lock()
	goi := make([]string, 0, len(p.cacGoi))
	for g := range p.cacGoi {
		goi = append(goi, g)
	}
	p.mu.Unlock()
	for _, g := range goi {
		k := khoaPhien(ma, g)
		p.phien.Xoa(k)
		p.loi.Xoa(k)
	}
}
