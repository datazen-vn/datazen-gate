package quyetdinh

import (
	"context"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/datazen-vn/datazen-gate/internal/dem"
	"github.com/datazen-vn/datazen-gate/internal/gopgoi"
	"github.com/datazen-vn/datazen-gate/internal/hub"
)

const (
	// HubCodeThueBaoHieuLuc: 409 khi tạo phiên cho khách đã có thuê bao `active` cùng gói (bản đệm quyền của gate cũ).
	HubCodeThueBaoHieuLuc = "thue-bao-dang-hieu-luc"
	// HubCodePhienDaNhanTien: 409 khi phiên `open` cũ sắp hết hạn cần thay nhưng ĐÃ có tiền: đưa khách về đúng phiên cũ.
	HubCodePhienDaNhanTien = "phien-da-nhan-tien"

	// Hub chỉ thay phiên khi còn < 10 phút tới expires_at; hub chưa báo replace_after thì tự tính: ngưỡng + 1 phút đệm.
	nguongThayPhien    = 10 * time.Minute
	truocHetHanDuPhong = nguongThayPhien + time.Minute
	// Trừ khỏi replace_after để lệch đồng hồ giữa gate và hub không đẩy khách vào phiên vừa bị thay.
	lechDongHo = 30 * time.Second
	// Tạo phiên lỗi thì nhớ lỗi chừng này (theo khách+gói) để mỗi yêu cầu bị chặn không đập hub; cũng là thời gian đệm URL phiên cũ.
	nhoLoi       = 5 * time.Second
	soPhienToiDa = 5_000
)

var maGoiRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)

// CauHinhPaywall của cổng: nhận ra điều hướng (302) khác lời gọi API (402), dựng `success_url`/`cancel_url` CHỈ từ origin đã khai.
type CauHinhPaywall struct {
	MaGoi         string
	PublicOrigins []string // origin công khai của app (https://host[:port]); 1–8
	LoginURL      string   // đường dẫn ("/login") hoặc URL tuyệt đối; rỗng = không có
	SuccessPath   string   // rỗng = chưa khai ("/" cho success_url, KHÔNG bypass)
	CancelPath    string   // rỗng = "/"
}

// Checkout là phần của hub mà paywall cần (hub.Khach thoả).
type Checkout interface {
	TaoPhien(ctx context.Context, y hub.TaoPhienYeuCau) (*hub.Phien, error)
	DocPhien(ctx context.Context, id string) (*hub.XemPhien, error)
}

type origin struct{ goc, host string }

// Paywall dựng URL quay về và đệm phiên thanh toán theo (khách, gói).
type Paywall struct {
	MaGoi string
	// Bypass: đường dẫn luôn bỏ qua kiểm quyền (login, success_path) — nếu không khách kẹt trước khi đăng nhập/quay về.
	Bypass   []string
	LoginURL string
	origins  []origin
	success  string
	cancel   string
	co       Checkout
	now      func() time.Time
	phien    *dem.Dem[string]
	loi      *dem.Dem[error]
	nhom     gopgoi.Nhom[string]
	mu       sync.Mutex // bảo vệ cacGoi
	cacGoi   map[string]bool
}

// MoiPaywall kiểm cấu hình rồi dựng Paywall. `now` nil ⇒ time.Now.
func MoiPaywall(c CauHinhPaywall, co Checkout, now func() time.Time) (*Paywall, error) {
	if ds := KiemPaywall(c); len(ds) > 0 {
		return nil, errors.New(strings.Join(ds, "; "))
	}
	if now == nil {
		now = time.Now
	}
	p := &Paywall{MaGoi: c.MaGoi, LoginURL: c.LoginURL, success: orMacDinh(c.SuccessPath, "/"), cancel: orMacDinh(c.CancelPath, "/"),
		co: co, now: now, phien: dem.Moi[string](soPhienToiDa, 0, now), loi: dem.Moi[error](soPhienToiDa, 0, now), cacGoi: map[string]bool{c.MaGoi: true}}
	for _, o := range c.PublicOrigins {
		og, _ := phanTichOrigin(o)
		p.origins = append(p.origins, og)
	}
	// Chỉ bypass trang quay về khi dev KHAI success_path (mặc định "/" mà bypass sẽ vô tình mở trang chủ).
	if c.SuccessPath != "" {
		p.Bypass = append(p.Bypass, c.SuccessPath)
	}
	if c.LoginURL != "" {
		if duongDanHopLe(c.LoginURL) {
			p.Bypass = append(p.Bypass, catQuery(c.LoginURL))
		} else if u, err := url.Parse(c.LoginURL); err == nil && p.coHost(strings.ToLower(u.Host)) {
			p.Bypass = append(p.Bypass, u.Path)
		}
	}
	return p, nil
}

func orMacDinh(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func catQuery(s string) string {
	if i := strings.IndexAny(s, "?#"); i >= 0 {
		return s[:i]
	}
	return s
}

func (p *Paywall) coHost(host string) bool {
	for _, o := range p.origins {
		if o.host == host {
			return true
		}
	}
	return false
}

// URLQuayVe: ỔN ĐỊNH — origin đã khai (khớp host của yêu cầu, không thì origin đầu) + đường dẫn cố định, KHÔNG đường dẫn/query của
// yêu cầu. Query có thể chứa token/email mà hub lưu nhiều năm; và phiên được đệm theo (khách, gói) nên một URL động sẽ trỏ về trang
// của yêu cầu đầu tiên chứ không phải trang hiện tại. `host` là Host THẬT của yêu cầu (không bao giờ tin X-Forwarded-Host).
type URLQuayVe struct{ Success, Cancel string }

func (p *Paywall) URLQuayVe(host string) URLQuayVe {
	goc := p.origins[0].goc
	for _, o := range p.origins {
		if o.host == strings.ToLower(host) {
			goc = o.goc
			break
		}
	}
	return URLQuayVe{Success: goc + p.success, Cancel: goc + p.cancel}
}
