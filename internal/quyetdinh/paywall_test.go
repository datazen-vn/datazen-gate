package quyetdinh

import (
	"reflect"
	"strings"
	"testing"
)

func TestKiemPaywall(t *testing.T) {
	hopLe := CauHinhPaywall{MaGoi: "pro", PublicOrigins: []string{"https://app.vn"}, LoginURL: "/login", SuccessPath: "/billing/return", CancelPath: "/pricing"}
	if ds := KiemPaywall(hopLe); len(ds) != 0 {
		t.Fatalf("hợp lệ mà báo lỗi: %v", ds)
	}
	cacTruongHop := map[string]func(*CauHinhPaywall){
		"plan_code hoa":               func(c *CauHinhPaywall) { c.MaGoi = "Pro" },
		"plan_code rỗng":              func(c *CauHinhPaywall) { c.MaGoi = "" },
		"không origin":                func(c *CauHinhPaywall) { c.PublicOrigins = nil },
		"9 origin":                    func(c *CauHinhPaywall) { c.PublicOrigins = make([]string, 9) },
		"origin http":                 func(c *CauHinhPaywall) { c.PublicOrigins = []string{"http://app.vn"} },
		"origin có đường dẫn":         func(c *CauHinhPaywall) { c.PublicOrigins = []string{"https://app.vn/x"} },
		"origin có query":             func(c *CauHinhPaywall) { c.PublicOrigins = []string{"https://app.vn?x=1"} },
		"origin có user:password":     func(c *CauHinhPaywall) { c.PublicOrigins = []string{"https://u:p@app.vn"} },
		"success_path không có /":     func(c *CauHinhPaywall) { c.SuccessPath = "ok" },
		"success_path //evil":         func(c *CauHinhPaywall) { c.SuccessPath = "//evil.test" },
		"success_path /\\evil":        func(c *CauHinhPaywall) { c.SuccessPath = "/\\evil" },
		"success_path có query":       func(c *CauHinhPaywall) { c.SuccessPath = "/ok?x=1" },
		"success_path có fragment":    func(c *CauHinhPaywall) { c.SuccessPath = "/ok#x" },
		"cancel_path có query":        func(c *CauHinhPaywall) { c.CancelPath = "/pricing?x=1" },
		"cancel_path có fragment":     func(c *CauHinhPaywall) { c.CancelPath = "/pricing#x" },
		"cancel_path có khoảng trắng": func(c *CauHinhPaywall) { c.CancelPath = "/a b" },
		"login_url //evil":            func(c *CauHinhPaywall) { c.LoginURL = "//evil.test" },
		"login_url http ngoài":        func(c *CauHinhPaywall) { c.LoginURL = "http://auth.app.vn/login" },
		"login_url không ASCII":       func(c *CauHinhPaywall) { c.LoginURL = "/đăng-nhập" },
	}
	for ten, sua := range cacTruongHop {
		c := hopLe
		sua(&c)
		if ds := KiemPaywall(c); len(ds) == 0 {
			t.Errorf("%s: phải bị từ chối", ten)
		}
	}
	// Liệt kê MỌI lỗi (cho --check), không dừng ở lỗi đầu.
	c := hopLe
	c.MaGoi, c.SuccessPath, c.CancelPath = "Pro", "ok", "x"
	if ds := KiemPaywall(c); len(ds) < 3 {
		t.Fatalf("phải liệt kê đủ 3 lỗi: %v", ds)
	}
	if _, err := MoiPaywall(c, nil, nil); err == nil || !strings.Contains(err.Error(), "plan_code") {
		t.Fatalf("MoiPaywall phải từ chối: %v", err)
	}
	// Hợp lệ: login_url tuyệt đối https, origin loopback có cổng.
	c = hopLe
	c.LoginURL = "https://auth.app.vn/login"
	c.PublicOrigins = []string{"https://app.vn", "http://localhost:3000"}
	if ds := KiemPaywall(c); len(ds) != 0 {
		t.Fatalf("%v", ds)
	}
}

func TestBypassPaths(t *testing.T) {
	cacTruongHop := []struct {
		ten  string
		c    CauHinhPaywall
		mong []string
	}{
		{"login + success", CauHinhPaywall{MaGoi: "pro", PublicOrigins: []string{"https://app.vn"}, LoginURL: "/login?next=/a#x", SuccessPath: "/billing/return"}, []string{"/billing/return", "/login"}},
		{"success mặc định KHÔNG bypass (không vô tình mở trang chủ)", CauHinhPaywall{MaGoi: "pro", PublicOrigins: []string{"https://app.vn"}}, nil},
		{"login tuyệt đối cùng host", CauHinhPaywall{MaGoi: "pro", PublicOrigins: []string{"https://app.vn"}, LoginURL: "https://app.vn/auth/login"}, []string{"/auth/login"}},
		{"login tuyệt đối khác host: không bypass", CauHinhPaywall{MaGoi: "pro", PublicOrigins: []string{"https://app.vn"}, LoginURL: "https://auth.other.vn/login"}, nil},
		{"cancel_path KHÔNG bypass", CauHinhPaywall{MaGoi: "pro", PublicOrigins: []string{"https://app.vn"}, CancelPath: "/pricing"}, nil},
	}
	for _, c := range cacTruongHop {
		p, err := MoiPaywall(c.c, nil, nil)
		if err != nil {
			t.Fatalf("%s: %v", c.ten, err)
		}
		if !reflect.DeepEqual(p.Bypass, c.mong) {
			t.Errorf("%s: bypass %v, mong %v", c.ten, p.Bypass, c.mong)
		}
	}
}

func TestURLQuayVeMacDinh(t *testing.T) {
	p, _ := MoiPaywall(CauHinhPaywall{MaGoi: "pro", PublicOrigins: []string{"https://app.vn"}}, nil, nil)
	if u := p.URLQuayVe("bat-ky.test"); u.Success != "https://app.vn/" || u.Cancel != "https://app.vn/" {
		t.Fatalf("mặc định successPath/cancelPath là /: %+v", u)
	}
}
