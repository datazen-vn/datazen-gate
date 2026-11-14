// Package chay ráp mọi thành phần của dz-gate (cấu hình → hub, danh tính, tuyến, paywall → proxy + cổng vận hành) và chạy tiến trình.
// Tách khỏi cmd/dz-gate để test được (runner conformance dùng chính Lap này).
package chay

import (
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/datazen-vn/datazen-gate/internal/cauhinh"
	"github.com/datazen-vn/datazen-gate/internal/hub"
	"github.com/datazen-vn/datazen-gate/internal/proxy"
	"github.com/datazen-vn/datazen-gate/internal/quyetdinh"
	"github.com/datazen-vn/datazen-gate/internal/tuyen"
	"github.com/datazen-vn/datazen-gate/internal/vanhanh"
)

// TuyChon của Lap: mọi thứ ngoài cấu hình (để test tiêm giả).
type TuyChon struct {
	Env      cauhinh.Env
	HTTP     *http.Client // cho hub, JWKS, session hook; nil ⇒ mặc định
	Now      func() time.Time
	PhienBan string
	Log      *slog.Logger // nil ⇒ bỏ log
	// Transport tới upstream; nil ⇒ proxy.DungTransport().
	Transport http.RoundTripper
	// QuanSat (tuỳ chọn) nhận thêm kết quả mỗi request (runner conformance lấy Quyet ở đây).
	QuanSat func(r *http.Request, kq quyetdinh.KetQua)
}

// Dich là các thành phần đã ráp.
type Dich struct {
	Chinh   http.Handler // cổng của khách (gate.listen)
	Ops     http.Handler // cổng vận hành (ops.listen)
	Cong    *cauhinh.Cong
	Do      *vanhanh.DoLuong
	SanSang *vanhanh.SanSang
}

// Lap kiểm cấu hình rồi ráp. Cấu hình đã qua MacDinh.
func Lap(cfg *cauhinh.Config, o TuyChon) (*Dich, error) {
	log := o.Log
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	do := vanhanh.MoiDoLuong()
	bam := vanhanh.MoiBam("")
	baoLoi := func(err error) { ghiLoi(log, do, err) }

	ua := "dz-gate/" + o.PhienBan
	cong, err := cfg.Dung(cauhinh.PhuThuoc{Env: o.Env, HTTP: o.HTTP, Now: o.Now, UserAgent: ua, BaoLoi: baoLoi})
	if err != nil {
		return nil, err
	}
	up, err := url.Parse(cfg.Gate.Upstream)
	if err != nil {
		return nil, err
	}
	hash := cfg.Ops.LogCustomerRef == "hash"
	tinCay, _ := cauhinh.ParseTrustedProxies(cfg.Gate.TrustedProxies) // đã qua Kiem ⇒ không hỏng
	logPath := cfg.Ops.LogPath
	pr := proxy.Moi(proxy.Cau{
		Cong: cong.Cong, Upstream: up, Xoa: cfg.XoaHeader(), CongKhai: congKhai(cfg), TrustedProxies: tinCay, GiuGachDuoi: cfg.Inject.AllowUnderscoreHeaders, Transport: o.Transport,
		QuanSat: func(r *http.Request, kq quyetdinh.KetQua, d time.Duration) {
			ghiRequest(log, do, bam, hash, logPath, r, kq, d)
			if o.QuanSat != nil {
				o.QuanSat(r, kq)
			}
		},
		BaoLoiUpstream: func(error) { do.Cong("dz_gate_upstream_errors_total") },
	})
	ss := vanhanh.MoiSanSang(o.Now,
		vanhanh.Kiem{Ten: "upstream", Fn: vanhanh.KiemTCP(diaChiTCP(up))},
		vanhanh.Kiem{Ten: "hub", Fn: cong.Khach.Ping},
	)
	return &Dich{Chinh: pr, Ops: vanhanh.Ops(do, ss), Cong: cong, Do: do, SanSang: ss}, nil
}

// congKhai: origin của paywall.public_origins (đã qua Kiem: https://host[:port]).
func congKhai(cfg *cauhinh.Config) []proxy.Origin {
	var ds []proxy.Origin
	for _, raw := range cfg.Paywall.PublicOrigins {
		if u, err := url.Parse(raw); err == nil && u.Host != "" {
			ds = append(ds, proxy.Origin{Scheme: u.Scheme, Host: strings.ToLower(u.Host)})
		}
	}
	return ds
}

func diaChiTCP(u *url.URL) string {
	port := u.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[u.Scheme]
	}
	return net.JoinHostPort(u.Hostname(), port)
}

// ketQuaNhan: nhãn ổn định (hữu hạn) cho metric/log.
func ketQuaNhan(kq quyetdinh.KetQua) string {
	if kq.Quyet.Cho {
		return "allowed"
	}
	return kq.Quyet.LyDo
}

func ghiRequest(log *slog.Logger, do *vanhanh.DoLuong, bam *vanhanh.Bam, hash bool, logPath string, r *http.Request, kq quyetdinh.KetQua, d time.Duration) {
	nhan := ketQuaNhan(kq)
	do.Cong("dz_gate_requests_total", "ket_qua", nhan)
	do.ThoiGian(d.Seconds())
	attrs := []any{"method", r.Method}
	if p, ok := truongPath(logPath, r, kq); ok {
		attrs = append(attrs, "path", p)
	}
	attrs = append(attrs, "ket_qua", nhan, "ms", d.Milliseconds())
	if kq.PhanHoi != nil {
		attrs = append(attrs, "status", kq.PhanHoi.Status)
	}
	if e := kq.Quyet.KetQua; e != nil {
		attrs = append(attrs, "nguon", e.Nguon)
	}
	if hash && kq.Quyet.MaKhach != "" {
		attrs = append(attrs, "cref_hash", bam.Khach(kq.Quyet.MaKhach))
	}
	log.Info("request", attrs...)
}

// truongPath: giá trị trường `path` theo ops.log_path; off ⇒ không có trường. route chỉ ghi nhãn tuyến từ cấu hình (không đường dẫn thật).
func truongPath(mode string, r *http.Request, kq quyetdinh.KetQua) (string, bool) {
	switch mode {
	case "off":
		return "", false
	case "full":
		return duongDanLog(r), true
	}
	if kq.Tuyen == "" {
		return "unmatched", true
	}
	return kq.Tuyen, true
}

// duongDanLog: đường dẫn THÔ bỏ query (query có thể mang token/email), cắt 200 ký tự.
func duongDanLog(r *http.Request) string {
	p := tuyen.DuongDanTho(r.RequestURI)
	if len(p) > 200 {
		p = p[:200]
	}
	return p
}

func ghiLoi(log *slog.Logger, do *vanhanh.DoLuong, err error) {
	if l, ok := hub.ALoi(err); ok {
		do.Cong("dz_gate_errors_total", "nguon", "hub", "ma", l.Ma)
		log.Warn("hub", "ma", l.Ma, "http", l.HTTP, "hub_code", l.HubCode)
		return
	}
	do.Cong("dz_gate_errors_total", "nguon", "danh_tinh")
	log.Debug("danh_tinh", "loi", err.Error())
}
