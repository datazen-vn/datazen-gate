package cauhinh

import (
	"strings"
	"testing"
	"time"
)

func envGia(kv map[string]string) Env {
	return func(ten string) (string, bool) { v, ok := kv[ten]; return v, ok }
}

var envTest = envGia(map[string]string{"DZ_API_KEY": "dz_test_khoa-abc123", "DZ_JWT_SECRET": strings.Repeat("s", 32)})

func docMau(t *testing.T) *Config {
	t.Helper()
	c, err := Doc(strings.NewReader(Mau))
	if err != nil {
		t.Fatal(err)
	}
	c.MacDinh()
	return c
}

func TestMauHopLe(t *testing.T) {
	c := docMau(t)
	kq := Kiem(c, envTest)
	if !kq.HopLe() {
		t.Fatalf("ví dụ phải hợp lệ: %v", kq.Cac)
	}
	for _, v := range kq.Cac {
		t.Logf("%s", v)
	}
	if len(c.Identity) != 2 || c.Hub.CacheTTL == nil || c.Hub.CacheTTL.D() != 60*time.Second || c.Identity[1].CacheTTL.D() != 30*time.Second {
		t.Fatalf("đọc sai: %+v", c)
	}
}

func TestDocNghiem(t *testing.T) {
	cacTruongHop := map[string]string{
		"trường lạ (gõ sai fail_mod)": "hub:\n  fail_mod: open\n",
		"thời lượng không đơn vị":     "hub:\n  cache_ttl: 60\n",
		"thời lượng hỏng":             "hub:\n  cache_ttl: 1 phut\n",
		"hai tài liệu":                "gate:\n  listen: \":1\"\n---\ngate:\n  listen: \":2\"\n",
		"rỗng":                        "",
		"kiểu sai":                    "routes: abc\n",
	}
	for ten, y := range cacTruongHop {
		if _, err := Doc(strings.NewReader(y)); err == nil {
			t.Errorf("%s: phải lỗi", ten)
		}
	}
	// JSON là tập con của YAML (runner conformance đưa config dạng JSON).
	c, err := Doc(strings.NewReader(`{"hub":{"cache_ttl":"60s","fail_mode":"closed"},"routes":[{"match":"/x","action":"require"}]}`))
	if err != nil || len(c.Routes) != 1 || c.Hub.CacheTTL.D() != time.Minute {
		t.Fatalf("JSON: %v %+v", err, c)
	}
}

func TestMacDinh(t *testing.T) {
	c := &Config{}
	c.MacDinh()
	if c.Gate.Mode != "proxy" || c.Hub.BaseURL != "https://api.datazen.vn" || c.Hub.APIKeyEnv != "DZ_API_KEY" || c.Hub.CacheTTL.D() != 60*time.Second ||
		c.Hub.FailMode != "closed" || c.Hub.Timeout.D() != 5*time.Second || c.DefaultAction != "allow" || c.Ops.Listen != "127.0.0.1:9090" || c.Ops.LogCustomerRef != "hash" {
		t.Fatalf("%+v", c)
	}
	// cache_ttl: 0s khai rõ = TẮT đệm, không bị mặc định đè.
	c2, _ := Doc(strings.NewReader("hub:\n  cache_ttl: 0s\n"))
	c2.MacDinh()
	if c2.Hub.CacheTTL.D() != 0 {
		t.Fatalf("0s phải giữ nguyên: %v", c2.Hub.CacheTTL.D())
	}
}

func TestKiemTungLuat(t *testing.T) {
	cacTruongHop := []struct {
		ten    string
		sua    func(*Config)
		env    Env
		truong string
	}{
		{"gate.listen sai", func(c *Config) { c.Gate.Listen = "8080" }, nil, "gate.listen"},
		{"gate.listen cổng 0", func(c *Config) { c.Gate.Listen = ":0" }, nil, "gate.listen"},
		{"gate.mode lạ", func(c *Config) { c.Gate.Mode = "tunnel" }, nil, "gate.mode"},
		{"thiếu upstream", func(c *Config) { c.Gate.Upstream = "" }, nil, "gate.upstream"},
		{"upstream không phải http", func(c *Config) { c.Gate.Upstream = "ftp://app" }, nil, "gate.upstream"},
		{"upstream user:password", func(c *Config) { c.Gate.Upstream = "http://u:p@app:3000" }, nil, "gate.upstream"},
		{"listen = upstream (cùng cổng loopback)", func(c *Config) { c.Gate.Listen = ":8080"; c.Gate.Upstream = "http://localhost:8080" }, nil, "gate.upstream"},
		{"listen = upstream (cùng host:port)", func(c *Config) { c.Gate.Listen = "app:3000"; c.Gate.Upstream = "http://app:3000" }, nil, "gate.upstream"},
		{"hub http ngoài loopback", func(c *Config) { c.Hub.BaseURL = "http://api.datazen.vn" }, nil, "hub.base_url"},
		{"hub fail_mode lạ", func(c *Config) { c.Hub.FailMode = "maybe" }, nil, "hub.fail_mode"},
		{"cache_ttl quá 10m", func(c *Config) { d := ThoiLuong(11 * time.Minute); c.Hub.CacheTTL = &d }, nil, "hub.cache_ttl"},
		{"stale_if_error quá 1h", func(c *Config) { c.Hub.StaleIfError = ThoiLuong(2 * time.Hour) }, nil, "hub.stale_if_error"},
		{"stale_if_error khi tắt đệm", func(c *Config) { z := ThoiLuong(0); c.Hub.CacheTTL = &z; c.Hub.StaleIfError = ThoiLuong(time.Minute) }, nil, "hub.stale_if_error"},
		{"timeout 0", func(c *Config) { c.Hub.Timeout = 0 }, nil, "hub.timeout"},
		{"timeout quá 60s", func(c *Config) { c.Hub.Timeout = ThoiLuong(2 * time.Minute) }, nil, "hub.timeout"},
		{"api_key_env sai tên", func(c *Config) { c.Hub.APIKeyEnv = "1 KHOA" }, nil, "hub.api_key_env"},
		{"thiếu biến khoá", nil, envGia(map[string]string{}), "hub.api_key_env"},
		{"khoá sai tiền tố", nil, envGia(map[string]string{"DZ_API_KEY": "sk_live_xyz"}), "hub.api_key_env"},
		{"khoá chỉ có tiền tố", nil, envGia(map[string]string{"DZ_API_KEY": "dz_test_"}), "hub.api_key_env"},
		{"không identity", func(c *Config) { c.Identity = nil }, nil, "identity"},
		{"identity type lạ", func(c *Config) { c.Identity[0].Type = "oauth" }, nil, "identity[0]"},
		{"jwt jwks http ngoài", func(c *Config) { c.Identity[0].JWKSURL = "http://app.vn/jwks" }, nil, "identity[0]"},
		{"jwt vừa jwks vừa secret", func(c *Config) { c.Identity[0].Secret = strings.Repeat("k", 32) }, nil, "identity[0]"},
		{"jwt secret_env thiếu", func(c *Config) { c.Identity[0].JWKSURL = ""; c.Identity[0].SecretEnv = "KHONG_CO" }, nil, "identity[0]"},
		{"jwt alg none", func(c *Config) { c.Identity[0].Algorithms = []string{"none"} }, nil, "identity[0]"},
		{"jwt from lạ", func(c *Config) { c.Identity[0].From = "query:t" }, nil, "identity[0]"},
		{"session_hook host ngoài", func(c *Config) { c.Identity[1].AllowHosts = nil }, nil, "identity[1]"},
		{"session_hook cache_ttl quá 10m", func(c *Config) { d := ThoiLuong(time.Hour); c.Identity[1].CacheTTL = &d }, nil, "identity[1]"},
		{"không có tuyến", func(c *Config) { c.Routes = nil }, nil, "routes"},
		{"tuyến match không có /", func(c *Config) { c.Routes[0].Match = "api" }, nil, "routes[0].match"},
		{"tuyến action lạ", func(c *Config) { c.Routes[0].Action = "deny" }, nil, "routes[0].action"},
		{"tuyến feature có dấu phẩy", func(c *Config) { c.Routes[1].Feature = "a,b" }, nil, "routes[1].feature"},
		{"tuyến plan_code hoa", func(c *Config) { c.Routes[1].PlanCode = "Pro" }, nil, "routes[1].plan_code"},
		{"tuyến fail_mode lạ", func(c *Config) { c.Routes[1].FailMode = "maybe" }, nil, "routes[1].fail_mode"},
		{"default_action lạ", func(c *Config) { c.DefaultAction = "deny" }, nil, "default_action"},
		{"paywall plan_code", func(c *Config) { c.Paywall.PlanCode = "Pro" }, nil, "paywall"},
		{"paywall origin http", func(c *Config) { c.Paywall.PublicOrigins = []string{"http://app.vn"} }, nil, "paywall"},
		{"paywall success_path có query", func(c *Config) { c.Paywall.SuccessPath = "/ok?x=1" }, nil, "paywall"},
		{"strip sai khuôn", func(c *Config) { c.Inject.StripIncoming = []string{"X-Foo"} }, nil, "inject.strip_incoming[0]"},
		{"ops.listen sai", func(c *Config) { c.Ops.Listen = "9090" }, nil, "ops.listen"},
		{"ops.listen = gate.listen", func(c *Config) { c.Ops.Listen = ":8080" }, nil, "ops.listen"},
		{"log_customer_ref lạ", func(c *Config) { c.Ops.LogCustomerRef = "raw" }, nil, "ops.log_customer_ref"},
	}
	for _, c := range cacTruongHop {
		cfg := docMau(t)
		if c.sua != nil {
			c.sua(cfg)
		}
		env := c.env
		if env == nil {
			env = envTest
		}
		kq := Kiem(cfg, env)
		if kq.HopLe() {
			t.Errorf("%s: phải có lỗi", c.ten)
			continue
		}
		co := false
		for _, v := range kq.Cac {
			if v.Muc == MucLoi && strings.HasPrefix(v.Truong, c.truong) {
				co = true
			}
		}
		if !co {
			t.Errorf("%s: mong lỗi ở %q, có %v", c.ten, c.truong, kq.Cac)
		}
	}
}

func TestKiemLietKeMoiLoi(t *testing.T) {
	c := docMau(t)
	c.Gate.Listen, c.Gate.Upstream = "x", ""
	c.Hub.BaseURL, c.Hub.FailMode = "http://evil", "maybe"
	c.Identity, c.Routes = nil, nil
	c.Paywall.PlanCode = "Pro"
	c.Inject.StripIncoming = []string{"BAD"}
	c.Ops.Listen = "x"
	kq := Kiem(c, envGia(nil))
	soLoi := 0
	for _, v := range kq.Cac {
		if v.Muc == MucLoi {
			soLoi++
		}
	}
	if soLoi < 10 {
		t.Fatalf("phải liệt kê ĐỦ mọi lỗi một lượt (≥ 10), có %d: %v", soLoi, kq.Cac)
	}
}

func TestKiemCanhBao(t *testing.T) {
	canh := func(c *Config, env Env) []string {
		var ds []string
		for _, v := range Kiem(c, env).Cac {
			if v.Muc == MucCanhBao {
				ds = append(ds, v.Truong)
			}
		}
		return ds
	}
	live := envGia(map[string]string{"DZ_API_KEY": "dz_live_khoa-abc123"})
	// Mẫu có tuyến fail_mode open + khoá live ⇒ cảnh báo, KHÔNG lỗi.
	c := docMau(t)
	if kq := Kiem(c, live); !kq.HopLe() || !contains(canh(c, live), "hub.fail_mode") {
		t.Fatalf("khoá live + fail_mode open: cảnh báo, không lỗi: %v", kq.Cac)
	}
	if contains(canh(docMau(t), envTest), "hub.fail_mode") {
		t.Fatal("khoá test không cần cảnh báo")
	}
	// jwt không issuer/audience.
	c = docMau(t)
	c.Identity[0].Issuer, c.Identity[0].Audience = "", ""
	if !contains(canh(c, envTest), "identity[0]") {
		t.Fatal("jwt thiếu issuer/audience phải cảnh báo")
	}
	// success_path khớp tuyến require.
	c = docMau(t)
	c.Routes = append(c.Routes, Route{Match: "/billing/*", Action: "require"})
	if !contains(canh(c, envTest), "paywall.success_path") {
		t.Fatal("success_path khớp require phải cảnh báo")
	}
	// tuyến allow có feature.
	c = docMau(t)
	c.Routes[0].Feature = "x"
	if !contains(canh(c, envTest), "routes[0].feature") {
		t.Fatal("allow + feature phải cảnh báo")
	}
}

func contains(ds []string, s string) bool {
	for _, d := range ds {
		if d == s {
			return true
		}
	}
	return false
}

func TestXoaHeader(t *testing.T) {
	c := docMau(t)
	got := c.XoaHeader()
	if len(got) != len(XoaMacDinh)+1 || got[len(got)-1] != "x-dz-*" {
		t.Fatalf("mặc định + strip_incoming của mẫu: %v", got)
	}
	// Mặc định PHẢI phủ nhóm danh tính/quyền và nhóm "nguồn gốc kết nối do client khai".
	for _, m := range []string{"x-dz-*", "x-auth-request-*", "x-forwarded-user", "forwarded", "x-forwarded-*", "x-real-ip", "true-client-ip", "cf-connecting-ip",
		"x-original-url", "x-rewrite-url", "x-original-uri", "x-original-host", "client-ip", "x-originating-ip", "x-host", "fly-client-ip"} {
		co := false
		for _, d := range XoaMacDinh {
			if d == m {
				co = true
			}
		}
		if !co {
			t.Errorf("XoaMacDinh thiếu %q", m)
		}
	}
}

func TestCanhBaoChoPhepHeaderGachDuoi(t *testing.T) {
	c := docMau(t)
	c.Inject.AllowUnderscoreHeaders = true
	kq := Kiem(c, envTest)
	if !kq.HopLe() {
		t.Fatal("chỉ cảnh báo, không lỗi")
	}
	co := false
	for _, v := range kq.Cac {
		if v.Muc == MucCanhBao && v.Truong == "inject.allow_underscore_headers" {
			co = true
		}
	}
	if !co {
		t.Fatalf("phải cảnh báo: %v", kq.Cac)
	}
}

func TestKiemTrustedProxies(t *testing.T) {
	co := func(ds []string, muc string) bool {
		c := docMau(t)
		c.Gate.TrustedProxies = ds
		for _, v := range Kiem(c, envTest).Cac {
			if v.Truong == "gate.trusted_proxies" && v.Muc == muc {
				return true
			}
		}
		return false
	}
	if co([]string{"10.0.0.0/8", "2001:db8::/32", "192.0.2.1", "::1"}, MucLoi) || co(nil, MucLoi) {
		t.Fatal("CIDR/IP hợp lệ và rỗng không được lỗi")
	}
	for _, bad := range []string{"rac", "10.0.0.0/33", "", "fe80::1%eth0", "10.0.0.0/8/1"} {
		if !co([]string{bad}, MucLoi) {
			t.Fatalf("%q phải là lỗi", bad)
		}
	}
	for _, rong := range []string{"0.0.0.0/0", "::/0"} {
		if !co([]string{rong}, MucCanhBao) || co([]string{rong}, MucLoi) {
			t.Fatalf("%s phải cảnh báo, không lỗi", rong)
		}
	}
	if co([]string{"10.0.0.0/8"}, MucCanhBao) {
		t.Fatal("dải hẹp không cảnh báo")
	}
}
