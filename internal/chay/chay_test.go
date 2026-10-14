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
