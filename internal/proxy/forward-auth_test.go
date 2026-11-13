package proxy

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"

	"github.com/datazen-vn/datazen-gate/internal/hub"
	"github.com/datazen-vn/datazen-gate/internal/quyetdinh"
)

// dungFA dựng handler forward-auth s; TinTu mặc định = loopback (client test nối từ 127.0.0.1).
func dungFA(t *testing.T, cong QuyetDinh, sua func(*CauForwardAuth)) *httptest.Server {
	c := CauForwardAuth{Cong: cong, Xoa: xoaMacDinh, TinTu: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, CongKhai: []Origin{{Scheme: "https", Host: "app.test"}}}
	if sua != nil {
		sua(&c)
	}
	s := httptest.NewServer(MoiForwardAuth(c))
	t.Cleanup(s.Close)
	return s
}

func goiFA(t *testing.T, s *httptest.Server, duong string, h map[string]string) *http.Response {
	req, _ := http.NewRequest("GET", s.URL+duong, nil)
	for k, v := range h {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { res.Body.Close() })
	return res
}

// Header đúng như từng proxy gửi tới máy chủ xác thực.
var kieuProxy = map[string]map[string]string{
	"traefik": {"X-Forwarded-Method": "POST", "X-Forwarded-Proto": "https", "X-Forwarded-Host": "app.test", "X-Forwarded-Uri": "/dashboard/a%2Fb?x=1", "X-Forwarded-For": "1.2.3.4"},
	"caddy":   {"X-Forwarded-Method": "POST", "X-Forwarded-Uri": "/dashboard/a%2Fb?x=1", "X-Forwarded-Host": "app.test", "X-Forwarded-Proto": "https"},
	// nginx auth_request: không có X-Forwarded-Method; method gốc là method của request con (ở đây GET).
	"nginx": {"X-Original-URI": "/dashboard/a%2Fb?x=1", "X-Forwarded-Host": "app.test"},
}

func TestForwardAuthDungRequestAoTheoTungKieuProxy(t *testing.T) {
	for ten, hd := range kieuProxy {
		t.Run(ten, func(t *testing.T) {
			var thay *http.Request
			cong := congGia(func(r *http.Request) quyetdinh.KetQua {
				thay = r
				return choQua("cus_1", &hub.KetQua{TrangThai: "active", MaGoi: "pro", Features: map[string]bool{"b": true, "a": true}, Nguon: "network"})(r)
			})
			s := dungFA(t, cong, nil)
			h := map[string]string{"Cookie": "sid=abc", "X-DZ-Customer-Ref": "gia", "X-DZ-Gate-Hop2": "x"}
			for k, v := range hd {
				h[k] = v
			}
			res := goiFA(t, s, "/_dz/auth", h)
			if res.StatusCode != 200 {
				t.Fatalf("%d", res.StatusCode)
			}
			wantMethod := map[string]string{"traefik": "POST", "caddy": "POST", "nginx": "GET"}[ten]
			if thay.Method != wantMethod || thay.RequestURI != "/dashboard/a%2Fb?x=1" || thay.Host != "app.test" || thay.URL.Path != "/dashboard/a/b" || thay.URL.RawQuery != "x=1" {
				t.Fatalf("request ảo sai: %s %s host=%s", thay.Method, thay.RequestURI, thay.Host)
			}
			if thay.Header.Get("Cookie") != "sid=abc" {
				t.Fatal("phải giữ Cookie của khách")
			}
			if thay.Header.Get("X-DZ-Customer-Ref") != "" || thay.Header.Get("X-Forwarded-For") != "" || thay.Header.Get("X-Original-Uri") != "" {
				t.Fatalf("header client/proxy phải bị xoá khỏi request ảo: %v", thay.Header)
			}
			if res.Header.Get("X-DZ-Customer-Ref") != "cus_1" || res.Header.Get("X-DZ-Plan") != "pro" || res.Header.Get("X-DZ-Features") != "a,b" ||
				res.Header.Get("X-DZ-Entitlement-Status") != "active" || res.Header.Get("X-DZ-Gate-Hop") != "" || res.Header.Get("Cache-Control") != "no-store" {
				t.Fatalf("header cho qua sai: %v", res.Header)
			}
		})
	}
}

func TestForwardAuthChanGiongCheDoProxy(t *testing.T) {
	ph := &quyetdinh.PhanHoi{Status: 402, Header: http.Header{"Content-Type": {"application/json"}, "Retry-After": {"5"}}, Than: []byte(`{"code":"dz_not_entitled"}`)}
	s := dungFA(t, congGia(func(*http.Request) quyetdinh.KetQua {
		return quyetdinh.KetQua{Quyet: quyetdinh.Quyet{LyDo: quyetdinh.LyDoKhongCoQuyen, Status: 402}, PhanHoi: ph}
	}), nil)
	res := goiFA(t, s, "/_dz/auth", kieuProxy["traefik"])
	if res.StatusCode != 402 || res.Header.Get("Retry-After") != "5" || res.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("phải trả nguyên phản hồi của lõi: %d %v", res.StatusCode, res.Header)
	}
}

func TestForwardAuthTuChoiKhongGoiLoi(t *testing.T) {
	var goi atomic.Int32
	cong := congGia(func(r *http.Request) quyetdinh.KetQua { goi.Add(1); return choQua("c", nil)(r) })
	nguoc := func(h map[string]string, k string) map[string]string {
		c := map[string]string{}
		for a, b := range h {
			if a != k {
				c[a] = b
			}
		}
		return c
	}
	t.Run("allow_from sai ⇒ 403", func(t *testing.T) {
		s := dungFA(t, cong, func(c *CauForwardAuth) { c.TinTu = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")} })
		if res := goiFA(t, s, "/_dz/auth", kieuProxy["traefik"]); res.StatusCode != 403 {
			t.Fatalf("%d", res.StatusCode)
		}
	})
	t.Run("allow_from rỗng ⇒ 403", func(t *testing.T) {
		s := dungFA(t, cong, func(c *CauForwardAuth) { c.TinTu = nil })
		if res := goiFA(t, s, "/_dz/auth", kieuProxy["nginx"]); res.StatusCode != 403 {
			t.Fatalf("%d", res.StatusCode)
		}
	})
	s := dungFA(t, cong, nil)
	t.Run("thiếu X-Forwarded-Uri ⇒ 400", func(t *testing.T) {
		if res := goiFA(t, s, "/_dz/auth", nguoc(kieuProxy["traefik"], "X-Forwarded-Uri")); res.StatusCode != 400 {
			t.Fatalf("%d", res.StatusCode)
		}
	})
	for ten, uri := range map[string]string{"không bắt đầu /": "dashboard", "//authority": "//evil.test/x", "khoảng trắng": "/a b"} {
		t.Run("URI hỏng: "+ten, func(t *testing.T) {
			h := nguoc(kieuProxy["traefik"], "X-Forwarded-Uri")
			h["X-Forwarded-Uri"] = uri
			if res := goiFA(t, s, "/_dz/auth", h); res.StatusCode != 400 {
				t.Fatalf("%d", res.StatusCode)
			}
		})
	}
	t.Run("Host không khớp public_origins ⇒ 400", func(t *testing.T) {
		h := nguoc(kieuProxy["traefik"], "X-Forwarded-Host")
		h["X-Forwarded-Host"] = "evil.test"
		if res := goiFA(t, s, "/_dz/auth", h); res.StatusCode != 400 {
			t.Fatalf("%d", res.StatusCode)
		}
		if res := goiFA(t, s, "/_dz/auth", nguoc(kieuProxy["traefik"], "X-Forwarded-Host")); res.StatusCode != 400 {
			t.Fatalf("thiếu host: %d", res.StatusCode)
		}
	})
	// nginx thật nhưng client tự gửi X-Forwarded-Uri/-Method giả: không được thắng X-Original-URI ⇒ 400, không gọi lõi.
	for ten, them := range map[string]map[string]string{
		"X-Forwarded-Uri giả":    {"X-Forwarded-Uri": "/public/x"},
		"X-Forwarded-Method giả": {"X-Forwarded-Method": "OPTIONS"},
		"cả hai giả":             {"X-Forwarded-Uri": "/public/x", "X-Forwarded-Method": "GET"},
	} {
		t.Run("nginx + "+ten+" ⇒ 400", func(t *testing.T) {
			h := nguoc(kieuProxy["nginx"], "")
			for a, b := range them {
				h[a] = b
			}
			if res := goiFA(t, s, "/_dz/auth", h); res.StatusCode != 400 {
				t.Fatalf("%d", res.StatusCode)
			}
		})
	}
	t.Run("traefik + X-Original-URI giả ⇒ 400", func(t *testing.T) {
		h := nguoc(kieuProxy["traefik"], "")
		h["X-Original-URI"] = "/public/x"
		if res := goiFA(t, s, "/_dz/auth", h); res.StatusCode != 400 {
			t.Fatalf("%d", res.StatusCode)
		}
	})
	t.Run("đường dẫn khác /_dz/auth ⇒ 404", func(t *testing.T) {
		if res := goiFA(t, s, "/_dz/healthz", kieuProxy["traefik"]); res.StatusCode != 404 {
			t.Fatalf("%d", res.StatusCode)
		}
	})
	if goi.Load() != 0 {
		t.Fatalf("lõi/hub bị gọi %d lần dù từ chối", goi.Load())
	}
}

func TestForwardAuthHeaderLapBiBoQua(t *testing.T) {
	// X-Forwarded-Uri đặt hai lần: không biết tin cái nào ⇒ 400, không đoán.
	s := dungFA(t, choQua("c", nil), nil)
	req, _ := http.NewRequest("GET", s.URL+"/_dz/auth", nil)
	req.Header["X-Forwarded-Uri"] = []string{"/a", "/b"}
	req.Header.Set("X-Forwarded-Host", "app.test")
	res, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("%d", res.StatusCode)
	}
}

func TestForwardAuthCheStatusChoNginx(t *testing.T) {
	ph := &quyetdinh.PhanHoi{Status: 402, Header: http.Header{"Content-Type": {"application/json"}}, Than: []byte(`{"code":"dz_not_entitled"}`)}
	s := dungFA(t, congGia(func(*http.Request) quyetdinh.KetQua {
		return quyetdinh.KetQua{Quyet: quyetdinh.Quyet{LyDo: quyetdinh.LyDoKhongCoQuyen, Status: 402}, PhanHoi: ph}
	}), nil)
	h := map[string]string{"X-Original-URI": "/dashboard/x", "X-Forwarded-Host": "app.test", HeaderCheStatus: "1"}
	res := goiFA(t, s, "/_dz/auth", h)
	if res.StatusCode != 401 || res.Header.Get(HeaderStatusThat) != "402" || res.ContentLength > 0 {
		t.Fatalf("che ⇒ 401 rỗng + mã thật: %d %v", res.StatusCode, res.Header)
	}
	delete(h, HeaderCheStatus)
	if res := goiFA(t, s, "/_dz/auth", h); res.StatusCode != 402 {
		t.Fatalf("không che ⇒ nguyên phản hồi: %d", res.StatusCode)
	}
	// Qua Caddy/Traefik (có X-Forwarded-Method) header che bị bỏ qua.
	delete(h, "X-Original-URI")
	h["X-Forwarded-Uri"], h["X-Forwarded-Method"] = "/dashboard/x", "GET"
	if res := goiFA(t, s, "/_dz/auth", h); res.StatusCode != 402 {
		t.Fatalf("có X-Forwarded-Method ⇒ không che: %d", res.StatusCode)
	}
	delete(h, "X-Forwarded-Method")
	delete(h, "X-Forwarded-Uri")
	h["X-Original-URI"] = "/dashboard/x"
	// Cho qua thì che không đổi gì: vẫn 200.
	ok := dungFA(t, choQua("c", nil), nil)
	h[HeaderCheStatus] = "1"
	if res := goiFA(t, ok, "/_dz/auth", h); res.StatusCode != 200 {
		t.Fatalf("%d", res.StatusCode)
	}
}
