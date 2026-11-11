package cauhinh

import (
	"net/http"
	"time"

	"github.com/datazen-vn/datazen-gate/internal/danhtinh"
	"github.com/datazen-vn/datazen-gate/internal/hub"
	"github.com/datazen-vn/datazen-gate/internal/quyetdinh"
	"github.com/datazen-vn/datazen-gate/internal/tuyen"
)

// PaywallQuyet chuyển phần paywall sang cấu hình của quyetdinh.
func (c *Config) PaywallQuyet() quyetdinh.CauHinhPaywall {
	p := c.Paywall
	return quyetdinh.CauHinhPaywall{MaGoi: p.PlanCode, PublicOrigins: p.PublicOrigins, LoginURL: p.LoginURL, SuccessPath: p.SuccessPath, CancelPath: p.CancelPath}
}

// DongTuyen chuyển `routes` sang bảng của tuyen.
func (c *Config) DongTuyen() []tuyen.Dong {
	ds := make([]tuyen.Dong, len(c.Routes))
	for i, r := range c.Routes {
		ds[i] = tuyen.Dong{Khop: r.Match, HanhDong: r.Action, TinhNang: r.Feature, MaGoi: r.PlanCode, CheDoLoi: r.FailMode}
	}
	return ds
}

// Khach dựng cấu hình client hub (khoá API đọc từ env).
func (c *Config) Khach(env Env, client *http.Client, now func() time.Time, userAgent string, onLoi func(*hub.Loi)) hub.CauHinh {
	khoa, _, _, _ := c.KhoaAPI(env) // Kiem đã báo mọi lỗi; ở đây khoá lỗi/thiếu thì là rỗng
	ttl := time.Duration(0)
	if c.Hub.CacheTTL != nil {
		ttl = c.Hub.CacheTTL.D()
	}
	return hub.CauHinh{
		BaseURL: c.Hub.BaseURL, APIKey: khoa, HTTP: client, Timeout: c.Hub.Timeout.D(), TTL: ttl,
		StaleIfError: c.Hub.StaleIfError.D(), CheDoLoi: c.Hub.FailMode, UserAgent: userAgent, Now: now, OnLoi: onLoi,
	}
}

// Bo dựng resolver danh tính thứ i (kiểm cấu hình đồng thời: lỗi cấu hình ⇒ error).
func (id Identity) Bo(env Env, client *http.Client, now func() time.Time) (danhtinh.Bo, error) {
	switch id.Type {
	case "jwt":
		return id.boJWT(env, client, now)
	case "session_hook":
		return id.boSessionHook(client, now)
	}
	return nil, errLoaiDanhTinh(id.Type)
}
