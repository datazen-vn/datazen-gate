package chay

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/datazen-vn/market-gate/internal/cauhinh"
)

func envThu(kv map[string]string) cauhinh.Env {
	return func(k string) (string, bool) { v, ok := kv[k]; return v, ok }
}

var envOK = envThu(map[string]string{"DZ_API_KEY": "dz_test_khoa-abc123"})

func viet(t *testing.T, noiDung string) string {
	t.Helper()
	p := t.TempDir() + "/gate.yaml"
	if err := os.WriteFile(p, []byte(noiDung), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func chayMain(args []string, env cauhinh.Env) (int, string, string) {
	var out, errb bytes.Buffer
	ma := Main(args, env, &out, &errb, "9.9.9")
	return ma, out.String(), errb.String()
}

func TestVersion(t *testing.T) {
	if ma, out, _ := chayMain([]string{"--version"}, envOK); ma != 0 || out != "dz-gate 9.9.9\n" {
		t.Fatalf("%d %q", ma, out)
	}
}

func TestViDuTrongDeployHopLeVaKhopMau(t *testing.T) {
	b, err := os.ReadFile("../../deploy/gate.example.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != cauhinh.Mau {
		t.Fatal("deploy/gate.example.yaml phải đúng bằng cauhinh.Mau (sửa một chỗ, chép sang chỗ kia)")
	}
	for _, args := range [][]string{{"--check", "../../deploy/gate.example.yaml"}, {"--check", "--config", "../../deploy/gate.example.yaml"}} {
		ma, out, errb := chayMain(args, envOK)
		if ma != 0 || !strings.Contains(out, "cấu hình hợp lệ") {
			t.Fatalf("%v: %d %q %q", args, ma, out, errb)
		}
	}
}

func TestCheckLietKeMoiLoi(t *testing.T) {
	p := viet(t, "gate:\n  listen: x\nhub:\n  fail_mode: maybe\nroutes: []\nidentity: []\npaywall:\n  plan_code: Pro\n")
	ma, _, errb := chayMain([]string{"--check", p}, envOK)
	if ma != MaSai {
		t.Fatalf("mã %d", ma)
	}
	for _, c := range []string{"gate.listen", "gate.upstream", "hub.fail_mode", "routes", "identity", "paywall", "KHÔNG hợp lệ"} {
		if !strings.Contains(errb, c) {
			t.Errorf("thiếu %q trong:\n%s", c, errb)
		}
	}
}

func TestCheckThieuKhoaApi(t *testing.T) {
	ma, _, errb := chayMain([]string{"--check", "../../deploy/gate.example.yaml"}, envThu(nil))
	if ma != MaSai || !strings.Contains(errb, "DZ_API_KEY") {
		t.Fatalf("%d %s", ma, errb)
	}
}

func TestKhongDocDuocTepLaMa2(t *testing.T) {
	if ma, _, errb := chayMain([]string{"--check", "/khong/co/tep.yaml"}, envOK); ma != MaKhongDoc || !strings.Contains(errb, "không đọc được") {
		t.Fatalf("%d %s", ma, errb)
	}
}

func TestThamSoSaiVaYamlHong(t *testing.T) {
	for _, args := range [][]string{{}, {"--khong-co"}, {"a.yaml", "b.yaml"}} {
		if ma, _, _ := chayMain(args, envOK); ma != MaSai {
			t.Errorf("%v: mã %d", args, ma)
		}
	}
	if ma, _, errb := chayMain([]string{"--check", viet(t, "hub:\n  fail_mod: open\n")}, envOK); ma != MaSai || !strings.Contains(errb, "fail_mod") {
		t.Fatalf("trường lạ phải là lỗi: %d %s", ma, errb)
	}
}

func TestHealthcheck(t *testing.T) {
	ops := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/_dz/healthz" {
			_, _ = w.Write([]byte("ok\n"))
			return
		}
		w.WriteHeader(500)
	}))
	defer ops.Close()
	listen := strings.TrimPrefix(ops.URL, "http://")
	p := viet(t, "ops:\n  listen: \""+listen+"\"\n")
	if ma, _, errb := chayMain([]string{"--healthcheck", "--config", p}, envOK); ma != 0 {
		t.Fatalf("sống phải 0: %d %s", ma, errb)
	}
	ops.Close()
	if ma, _, _ := chayMain([]string{"--healthcheck", "--config", p}, envOK); ma != MaSai {
		t.Fatalf("chết phải 1: %d", ma)
	}
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer bad.Close()
	p2 := viet(t, "ops:\n  listen: \""+strings.TrimPrefix(bad.URL, "http://")+"\"\n")
	if ma, _, _ := chayMain([]string{"--healthcheck", "--config", p2}, envOK); ma != MaSai {
		t.Fatalf("503 phải 1: %d", ma)
	}
}

func TestLapVaChayMotVongQuaProxy(t *testing.T) {
	hubGia := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ma := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/customers/"), "/entitlements")
		if r.Header.Get("Authorization") != "Bearer dz_test_khoa-abc123" || !strings.HasPrefix(r.Header.Get("User-Agent"), "dz-gate/9.9.9") {
			w.WriteHeader(401)
			return
		}
		st := "none"
		if ma == "cus_tra" {
			st = "active"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"customer_ref": ma, "status": st, "plan_code": "pro", "features": map[string]bool{"dashboard": st == "active"}})
	}))
	defer hubGia.Close()
	var nhanDuoc http.Header
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nhanDuoc = r.Header.Clone()
		_, _ = w.Write([]byte("app"))
	}))
	defer up.Close()
	cfg, err := cauhinh.Doc(strings.NewReader(`{
	  "gate": {"listen": ":18080", "upstream": "` + up.URL + `"},
	  "hub": {"base_url": "` + hubGia.URL + `"},
	  "routes": [{"match": "/dashboard/*", "action": "require", "feature": "dashboard"}],
	  "paywall": {"plan_code": "pro", "public_origins": ["https://app.test"]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	cfg.MacDinh()
	// danh tính: JWT HS256 (không cần máy chủ JWKS)
	cfg.Identity = []cauhinh.Identity{{Type: "jwt", Secret: strings.Repeat("s", 32), Issuer: "i", Audience: "a"}}
	d, err := Lap(cfg, TuyChon{Env: envOK, HTTP: hubGia.Client(), PhienBan: "9.9.9"})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(d.Chinh)
	defer s.Close()
	res, err := http.Get(s.URL + "/dashboard/x")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("chưa đăng nhập ⇒ 401, có %d", res.StatusCode)
	}
	if nhanDuoc != nil {
		t.Fatal("chặn thì không gọi upstream")
	}
	// Dạng tuyệt đối của request-line (`GET http://host/path`): quyết định vẫn theo ĐƯỜNG DẪN, không bị scheme://host đánh lạc hướng.
	c, err := net.Dial("tcp", strings.TrimPrefix(s.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	_, _ = c.Write([]byte("GET http://app.test/dashboard/x HTTP/1.1\r\nHost: app.test\r\nConnection: close\r\n\r\n"))
	hang, _ := bufio.NewReader(c).ReadString('\n')
	c.Close()
	if !strings.HasPrefix(hang, "HTTP/1.1 401") {
		t.Fatalf("dạng tuyệt đối phải bị chặn như thường: %q", hang)
	}
	if nhanDuoc != nil {
		t.Fatal("chặn thì không gọi upstream")
	}
	res, _ = http.Get(s.URL + "/linh-tinh")
	res.Body.Close()
	if res.StatusCode != 200 || nhanDuoc.Get("X-DZ-Gate-Hop") != "1" {
		t.Fatalf("tuyến allow mặc định qua: %d %v", res.StatusCode, nhanDuoc)
	}
	// số đo và cổng vận hành phản ánh
	var m bytes.Buffer
	d.Do.Viet(&m)
	if !strings.Contains(m.String(), `dz_gate_requests_total{ket_qua="allowed"} 1`) || !strings.Contains(m.String(), `dz_gate_requests_total{ket_qua="no_identity"} 2`) {
		t.Fatalf("metric:\n%s", m.String())
	}
	ops := httptest.NewServer(d.Ops)
	defer ops.Close()
	r, _ := http.Get(ops.URL + "/_dz/readyz")
	var body struct {
		Ready  bool
		Checks map[string]string
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	r.Body.Close()
	if !body.Ready || body.Checks["upstream"] != "ok" || body.Checks["hub"] != "ok" {
		t.Fatalf("readyz: %d %+v", r.StatusCode, body)
	}
}

func TestLapTuChoiCauHinhSai(t *testing.T) {
	cfg, _ := cauhinh.Doc(strings.NewReader("hub:\n  fail_mode: maybe\n"))
	cfg.MacDinh()
	if _, err := Lap(cfg, TuyChon{Env: envOK}); err == nil {
		t.Fatal("cấu hình sai phải bị từ chối")
	}
}

func TestVongLap508VaGiaMaoHeaderQuaProxyThat(t *testing.T) {
	// T4 (panel an ninh): kiểm X-DZ-Gate-Hop TRƯỚC khi xoá header x-dz-*; sau khi xoá thì đặt Hop mới vào request gửi upstream.
	hubGia := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"customer_ref": "cus_1", "status": "active", "plan_code": "pro", "features": map[string]bool{"dashboard": true}})
	}))
	defer hubGia.Close()
	var goi int
	var nhan http.Header
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { goi++; nhan = r.Header.Clone() }))
	defer up.Close()
	cfg, _ := cauhinh.Doc(strings.NewReader(`{
	  "gate": {"listen": ":18081", "upstream": "` + up.URL + `"}, "hub": {"base_url": "` + hubGia.URL + `"},
	  "routes": [{"match": "/dashboard/*", "action": "require", "feature": "dashboard"}],
	  "paywall": {"plan_code": "pro", "public_origins": ["https://app.test"]}}`))
	cfg.MacDinh()
	cfg.Identity = []cauhinh.Identity{{Type: "jwt", Secret: strings.Repeat("s", 32), Issuer: "i", Audience: "a"}}
	d, err := Lap(cfg, TuyChon{Env: envOK, HTTP: hubGia.Client(), PhienBan: "t"})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(d.Chinh)
	defer s.Close()

	req, _ := http.NewRequest("GET", s.URL+"/mo", nil)
	req.Header.Set("X-DZ-Gate-Hop", "1")
	res, _ := http.DefaultClient.Do(req)
	res.Body.Close()
	if res.StatusCode != 508 || goi != 0 {
		t.Fatalf("có Gate-Hop ở request đến ⇒ 508, không gọi upstream (kể cả tuyến không cần quyền): %d, upstream %d lần", res.StatusCode, goi)
	}
	req, _ = http.NewRequest("GET", s.URL+"/mo", nil)
	req.Header.Set("X-DZ-Customer-Ref", "admin")
	req.Header.Set("x-dz-entitlement-status", "active")
	res, _ = http.DefaultClient.Do(req)
	res.Body.Close()
	if res.StatusCode != 200 || goi != 1 || nhan.Get("X-DZ-Customer-Ref") != "" || nhan.Get("X-DZ-Entitlement-Status") != "" || nhan.Get("X-DZ-Gate-Hop") != "1" {
		t.Fatalf("header giả bị xoá, Hop mới do gate đặt: %d %v", res.StatusCode, nhan)
	}
}

func TestLapNoiPublicOriginsVaoXForwardedHostVaVungDZ(t *testing.T) {
	var nhan http.Header
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { nhan = r.Header.Clone() }))
	defer up.Close()
	cfg, _ := cauhinh.Doc(strings.NewReader(`{
	  "gate": {"listen": ":18082", "upstream": "` + up.URL + `"}, "hub": {"base_url": "http://127.0.0.1:1"},
	  "routes": [{"match": "/dashboard/*", "action": "require"}],
	  "paywall": {"plan_code": "pro", "public_origins": ["https://app.test", "https://www.app.test"]}}`))
	cfg.MacDinh()
	cfg.Identity = []cauhinh.Identity{{Type: "jwt", Secret: strings.Repeat("s", 32), Issuer: "i", Audience: "a"}}
	d, err := Lap(cfg, TuyChon{Env: envOK, PhienBan: "t"})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(d.Chinh)
	defer s.Close()
	c, _ := net.Dial("tcp", strings.TrimPrefix(s.URL, "http://"))
	_, _ = c.Write([]byte("GET /mo HTTP/1.1\r\nHost: evil.test\r\nX-Forwarded-Host: x\r\nConnection: close\r\n\r\n"))
	_, _ = bufio.NewReader(c).ReadString('\n')
	c.Close()
	if nhan.Get("X-Forwarded-Host") != "app.test" || nhan.Get("X-Forwarded-Proto") != "https" {
		t.Fatalf("X-Forwarded-* phải theo public_origins: %v", nhan)
	}
	nhan = nil
	res, _ := http.Get(s.URL + "/_dz/healthz")
	res.Body.Close()
	if res.StatusCode != 404 || nhan != nil {
		t.Fatalf("/_dz/* ở mặt công khai ⇒ 404, không tới app: %d", res.StatusCode)
	}
}

func TestLapNoiAllowUnderscoreHeaders(t *testing.T) {
	for _, cho := range []bool{false, true} {
		var nhan http.Header
		up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { nhan = r.Header.Clone() }))
		cfg, _ := cauhinh.Doc(strings.NewReader(`{
		  "gate": {"listen": ":18083", "upstream": "` + up.URL + `"}, "hub": {"base_url": "http://127.0.0.1:1"},
		  "routes": [{"match": "/dashboard/*", "action": "require"}], "inject": {"allow_underscore_headers": ` + map[bool]string{true: "true", false: "false"}[cho] + `},
		  "paywall": {"plan_code": "pro", "public_origins": ["https://app.test"]}}`))
		cfg.MacDinh()
		cfg.Identity = []cauhinh.Identity{{Type: "jwt", Secret: strings.Repeat("s", 32), Issuer: "i", Audience: "a"}}
		d, err := Lap(cfg, TuyChon{Env: envOK, PhienBan: "t"})
		if err != nil {
			t.Fatal(err)
		}
		s := httptest.NewServer(d.Chinh)
		req, _ := http.NewRequest("GET", s.URL+"/mo", nil)
		req.Header.Set("X_Custom", "1")
		req.Header.Set("X_DZ_Customer_Ref", "admin")
		res, _ := http.DefaultClient.Do(req)
		res.Body.Close()
		s.Close()
		up.Close()
		if (nhan.Get("X_Custom") == "1") != cho || nhan.Get("X_DZ_Customer_Ref") != "" {
			t.Errorf("allow_underscore_headers=%v: %v", cho, nhan)
		}
	}
}

// DZ_API_KEY_FILE: khoá đọc từ tệp (Docker/Kubernetes secrets) phải tới hub y như khoá từ biến môi trường, và --check chấp nhận nó.
func TestKhoaApiTuTepToiHubVaCheckChapNhan(t *testing.T) {
	const khoa = "dz_test_khoa-tu-tep-abc123"
	tep := t.TempDir() + "/dz_api_key"
	if err := os.WriteFile(tep, []byte(khoa+"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	envTep := envThu(map[string]string{"DZ_API_KEY_FILE": tep})
	if ma, out, errb := chayMain([]string{"--check", "../../deploy/gate.example.yaml"}, envTep); ma != MaDat || !strings.Contains(out, "cấu hình hợp lệ") {
		t.Fatalf("--check với khoá từ tệp: %d %q %q", ma, out, errb)
	}
	var thay string
	hubGia := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		thay = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"customer_ref": "cus_x", "status": "none"})
	}))
	defer hubGia.Close()
	cfg, err := cauhinh.Doc(strings.NewReader(`{"gate": {"listen": ":18081", "upstream": "http://127.0.0.1:1"}, "hub": {"base_url": "` + hubGia.URL + `"},
	  "routes": [{"match": "/x", "action": "allow"}], "paywall": {"plan_code": "pro", "public_origins": ["https://app.test"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	cfg.MacDinh()
	cfg.Identity = []cauhinh.Identity{{Type: "jwt", Secret: strings.Repeat("s", 32), Issuer: "i", Audience: "a"}}
	d, err := Lap(cfg, TuyChon{Env: envTep, HTTP: hubGia.Client(), PhienBan: "9.9.9"})
	if err != nil {
		t.Fatal(err)
	}
	_ = d.Cong.Khach.Ping(t.Context())
	if thay != "Bearer "+khoa {
		t.Fatalf("hub phải nhận khoá từ tệp (đã cắt xuống dòng), nhận %q", thay)
	}
}
