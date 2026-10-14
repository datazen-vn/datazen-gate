package cauhinh

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/datazen-vn/market-gate/internal/danhtinh"
	"github.com/datazen-vn/market-gate/internal/hub"
	"github.com/datazen-vn/market-gate/internal/quyetdinh"
	"github.com/datazen-vn/market-gate/internal/tuyen"
)

// XoaMacDinh: header luôn bị xoá khỏi yêu cầu đến, kể cả khi dev không khai strip_incoming. Hai nhóm:
//   - danh tính/quyền (`x-dz-*`, `x-auth-request-*`, `x-forwarded-user`): upstream tin chúng;
//   - nguồn gốc kết nối do client tự khai (`forwarded`, `x-forwarded-*` MỌI biến thể kể cả -port/-prefix/-ssl/-scheme/-server, `x-real-ip`,
//     `true-client-ip`, `cf-connecting-ip`…): app tin chúng để dựng IP/URL/ratelimit. Gate xoá TRƯỚC rồi mới tự đặt X-Forwarded-For/-Host/
//     -Proto từ kết nối thật (không tin LB: xem follow-up `trusted_proxies`).
var XoaMacDinh = []string{
	"x-dz-*", "x-auth-request-*", "x-forwarded-user",
	"forwarded", "x-forwarded-*", "x-original-forwarded-*", "x-real-ip", "x-client-ip", "x-cluster-client-ip",
	"true-client-ip", "cf-connecting-ip", "fastly-client-ip",
}

// XoaHeader trả toàn bộ mẫu header cần xoá khỏi yêu cầu đến (mặc định + strip_incoming).
func (c *Config) XoaHeader() []string {
	return append(append([]string{}, XoaMacDinh...), c.Inject.StripIncoming...)
}

// PhuThuoc của Dung: mọi thứ ngoài cấu hình mà thành phần cần (để test tiêm giả).
type PhuThuoc struct {
	Env       Env
	HTTP      *http.Client
	Now       func() time.Time
	UserAgent string
	// BaoLoi nhận mọi lỗi (hub bị cho qua, danh tính, tạo phiên…) để log/metric.
	BaoLoi func(error)
}

// Cong là bộ thành phần đã dựng.
type Cong struct {
	*quyetdinh.Cong
	Khach *hub.Khach
}

// Dung kiểm cấu hình rồi dựng Cong. Cấu hình phải đã qua MacDinh.
func (c *Config) Dung(p PhuThuoc) (*Cong, error) {
	if p.Env == nil {
		return nil, errors.New("thiếu Env")
	}
	if kq := Kiem(c, p.Env); !kq.HopLe() {
		return nil, fmt.Errorf("cấu hình không hợp lệ: %s", kq.Cac[firstLoi(kq)])
	}
	khach := hub.Moi(c.Khach(p.Env, p.HTTP, p.Now, p.UserAgent, func(l *hub.Loi) {
		if p.BaoLoi != nil {
			p.BaoLoi(l)
		}
	}))
	pw, err := quyetdinh.MoiPaywall(c.PaywallQuyet(), khach, p.Now)
	if err != nil {
		return nil, err
	}
	var bo []danhtinh.Bo
	for i, id := range c.Identity {
		b, err := id.Bo(p.Env, p.HTTP, p.Now)
		if err != nil {
			return nil, fmt.Errorf("identity[%d]: %w", i, err)
		}
		bo = append(bo, b)
	}
	bypass := append(append([]string{}, tuyen.BypassMacDinh...), pw.Bypass...)
	return &Cong{
		Khach: khach,
		Cong: &quyetdinh.Cong{
			Quyen: khach, DanhTinh: bo, Bang: tuyen.TaoBang(bypass, c.DongTuyen(), c.DefaultAction),
			Paywall: pw, CheDoLoi: c.Hub.FailMode, BaoLoi: p.BaoLoi,
		},
	}, nil
}

func firstLoi(k *KetQuaKiem) int {
	for i, v := range k.Cac {
		if v.Muc == MucLoi {
			return i
		}
	}
	return 0
}
