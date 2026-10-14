// Package chay ráp mọi thành phần của dz-gate (cấu hình → hub, danh tính, tuyến, paywall → proxy + cổng vận hành) và chạy tiến trình.
// Tách khỏi cmd/dz-gate để test được (runner conformance dùng chính Lap này).
package chay

import (
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/datazen-vn/market-gate/internal/cauhinh"
	"github.com/datazen-vn/market-gate/internal/hub"
	"github.com/datazen-vn/market-gate/internal/proxy"
	"github.com/datazen-vn/market-gate/internal/quyetdinh"
	"github.com/datazen-vn/market-gate/internal/tuyen"
	"github.com/datazen-vn/market-gate/internal/vanhanh"
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
	pr := proxy.Moi(proxy.Cau{
		Cong: cong.Cong, Upstream: up, Xoa: cfg.XoaHeader(), Transport: o.Transport,
		QuanSat: func(r *http.Request, kq quyetdinh.KetQua, d time.Duration) {
			ghiRequest(log, do, bam, hash, r, kq, d)
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

func ghiRequest(log *slog.Logger, do *vanhanh.DoLuong, bam *vanhanh.Bam, hash bool, r *http.Request, kq quyetdinh.KetQua, d time.Duration) {
	nhan := ketQuaNhan(kq)
	do.Cong("dz_gate_requests_total", "ket_qua", nhan)
	do.ThoiGian(d.Seconds())
	attrs := []any{"method", r.Method, "path", duongDanLog(r), "ket_qua", nhan, "ms", d.Milliseconds()}
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
