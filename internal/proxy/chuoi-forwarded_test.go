package proxy

import (
	"net/http"
	"net/netip"
	"strings"
	"testing"
)

func cidr(t *testing.T, ds ...string) []netip.Prefix {
	var ps []netip.Prefix
	for _, d := range ds {
		ps = append(ps, netip.MustParsePrefix(d))
	}
	return ps
}

// Kết nối test tới từ 127.0.0.1 (hoặc ::1 nếu ép) — "LB" là loopback.
func TestTrustedProxiesXFF(t *testing.T) {
	loopback := []string{"127.0.0.0/8"}
	bang := []struct {
		ten    string
		tin    []string
		xff    []string
		proto  string
		wantIP string
		wantPr string
	}{
		{"không khai ⇒ như cũ (bỏ XFF client)", nil, []string{"6.6.6.6"}, "https", "127.0.0.1", "http"},
		{"LB tin: IP khách", loopback, []string{"203.0.113.9"}, "https", "203.0.113.9", "https"},
		{"LB tin: giả XFF phía trái bị bỏ", loopback, []string{"6.6.6.6, 203.0.113.9"}, "", "203.0.113.9", "http"},
		{"nhiều hop, bỏ hop tin cậy", []string{"127.0.0.0/8", "10.0.0.0/8"}, []string{"198.51.100.7, 10.1.1.1, 10.2.2.2"}, "", "198.51.100.7", "http"},
		{"nhiều dòng header", []string{"127.0.0.0/8", "10.0.0.0/8"}, []string{"1.1.1.1", "198.51.100.7, 10.1.1.1"}, "", "198.51.100.7", "http"},
		{"IPv6 khách", loopback, []string{"2001:db8::1"}, "", "2001:db8::1", "http"},
		{"IPv6 hop tin cậy", []string{"127.0.0.0/8", "2001:db8:ffff::/48"}, []string{"192.0.2.5, 2001:db8:ffff::7"}, "", "192.0.2.5", "http"},
		{"IPv4-mapped IPv6", loopback, []string{"::ffff:203.0.113.9"}, "", "203.0.113.9", "http"},
		{"mọi hop đều tin ⇒ ngoài cùng bên trái", []string{"127.0.0.0/8", "10.0.0.0/8"}, []string{"10.9.9.9, 10.1.1.1"}, "", "10.9.9.9", "http"},
		{"rác ⇒ RemoteAddr", loopback, []string{"abc"}, "", "127.0.0.1", "http"},
		{"rác trái IP khách (không bao giờ đọc tới) bị bỏ", loopback, []string{"abc, 203.0.113.9"}, "", "203.0.113.9", "http"},
		{"rác sát phải ⇒ RemoteAddr", loopback, []string{"203.0.113.9, <script>"}, "", "127.0.0.1", "http"},
		{"quá dài ⇒ RemoteAddr", loopback, []string{strings.Repeat("1.2.3.4, ", 200) + "203.0.113.9"}, "", "127.0.0.1", "http"},
		{"proto lạ bị bỏ", loopback, []string{"203.0.113.9"}, "gopher", "203.0.113.9", "http"},
		{"proto nhiều token lấy cuối", loopback, []string{"203.0.113.9"}, "http, HTTPS", "203.0.113.9", "https"},
		{"không có XFF ⇒ RemoteAddr", loopback, nil, "https", "127.0.0.1", "https"},
		{"kết nối ngoài CIDR, XFF + proto giả bị xoá", []string{"10.0.0.0/8"}, []string{"6.6.6.6"}, "https", "127.0.0.1", "http"},
	}
	for _, c := range bang {
		t.Run(c.ten, func(t *testing.T) {
			u := moiUpstream(t, nil)
			s := dungProxy(t, u, choQua("c", nil), func(cau *Cau) { cau.TrustedProxies = cidr(t, c.tin...) })
			req, _ := http.NewRequest("GET", s.URL+"/x", nil)
			for _, v := range c.xff {
				req.Header.Add("X-Forwarded-For", v)
			}
			if c.proto != "" {
				req.Header.Set("X-Forwarded-Proto", c.proto)
			}
			req.Header.Set("Forwarded", "for=6.6.6.6")
			req.Header.Set("X-Real-IP", "6.6.6.6")
			req.Header.Set("CF-Connecting-IP", "6.6.6.6")
			req.Header.Set("True-Client-IP", "6.6.6.6")
			req.Header.Set("X-Forwarded-Host", "ke-tan-cong.test")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			got := u.cuoi(t).Header
			if got.Get("X-Forwarded-For") != c.wantIP || got.Get("X-Forwarded-Proto") != c.wantPr {
				t.Fatalf("XFF=%q proto=%q, muốn %q/%q", got.Get("X-Forwarded-For"), got.Get("X-Forwarded-Proto"), c.wantIP, c.wantPr)
			}
			for _, h := range []string{"Forwarded", "X-Real-IP", "Cf-Connecting-Ip", "True-Client-Ip"} {
				if got.Get(h) != "" {
					t.Fatalf("%s phải bị xoá, có %q", h, got.Get(h))
				}
			}
			if strings.Contains(got.Get("X-Forwarded-Host"), "ke-tan-cong") {
				t.Fatal("không tin X-Forwarded-Host của LB")
			}
		})
	}
}

// Host vẫn theo public_origins dù LB tin cậy gửi X-Forwarded-Host/Proto khác; Proto http|https của LB tin cậy thắng scheme origin.
func TestTrustedProxiesHostVanTheoPublicOrigins(t *testing.T) {
	u := moiUpstream(t, nil)
	s := dungProxy(t, u, choQua("c", nil), func(c *Cau) {
		c.TrustedProxies = cidr(t, "127.0.0.0/8")
		c.CongKhai = []Origin{{Scheme: "https", Host: "app.test"}}
	})
	req, _ := http.NewRequest("GET", s.URL+"/x", nil)
	req.Header.Set("X-Forwarded-Host", "ke-tan-cong.test")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.Header.Set("X-Forwarded-Proto", "gopher")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	got := u.cuoi(t).Header
	if got.Get("X-Forwarded-Host") != "app.test" || got.Get("X-Forwarded-Proto") != "https" || got.Get("X-Forwarded-For") != "203.0.113.9" {
		t.Fatalf("sai: %v", got)
	}
}
