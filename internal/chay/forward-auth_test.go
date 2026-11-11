package chay

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/datazen-vn/datazen-gate/internal/cauhinh"
)

// lapFA ráp gate forward-auth thật (Lap) với hub giả đếm lời gọi và session hook giả.
func lapFA(t *testing.T, hook http.HandlerFunc, allowFrom string) (*httptest.Server, *atomic.Int32) {
	var hubGoi atomic.Int32
	hubGia := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hubGoi.Add(1); w.WriteHeader(500) }))
	t.Cleanup(hubGia.Close)
	hk := httptest.NewServer(hook)
	t.Cleanup(hk.Close)
	cfg, err := cauhinh.Doc(strings.NewReader(`{
	  "gate": {"listen": ":18082", "mode": "forward-auth", "forward_auth": {"allow_from": ["` + allowFrom + `"]}},
	  "hub": {"base_url": "` + hubGia.URL + `"},
	  "identity": [{"type": "session_hook", "url": "` + hk.URL + `/me", "subject_path": "user.id"}],
	  "routes": [{"match": "/api/public/*", "action": "allow"}, {"match": "/dashboard/*", "action": "require", "feature": "dashboard"}],
	  "paywall": {"plan_code": "pro", "public_origins": ["https://app.test"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	cfg.MacDinh()
	if kq := cauhinh.Kiem(cfg, envOK); !kq.HopLe() {
		t.Fatalf("cấu hình forward-auth phải hợp lệ: %v", kq.Cac)
	}
	d, err := Lap(cfg, TuyChon{Env: envOK, HTTP: hubGia.Client(), PhienBan: "t"})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(d.Chinh)
	t.Cleanup(s.Close)
	return s, &hubGoi
}

func hoiFA(t *testing.T, s *httptest.Server, uri, cookie string) *http.Response {
	req, _ := http.NewRequest("GET", s.URL+"/_dz/auth", nil)
	req.Header.Set("X-Forwarded-Uri", uri)
	req.Header.Set("X-Forwarded-Host", "app.test")
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	res, err := http.DefaultTransport.RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	return res
}

func TestForwardAuthQuaLapThat(t *testing.T) {
	s, hubGoi := lapFA(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }, "127.0.0.0/8")
	if res := hoiFA(t, s, "/api/public/x", ""); res.StatusCode != 200 {
		t.Fatalf("tuyến allow ⇒ 200, có %d", res.StatusCode)
	}
	if res := hoiFA(t, s, "/dashboard/x", ""); res.StatusCode != 401 {
		t.Fatalf("chưa đăng nhập ⇒ 401 (cùng chế độ proxy), có %d", res.StatusCode)
	}
	// Danh tính không xác định (session hook 500) ⇒ 503 + Retry-After, KHÔNG fail-open, không chạm hub.
	res := hoiFA(t, s, "/dashboard/x", "sid=abc")
	if res.StatusCode != 503 || res.Header.Get("Retry-After") != "5" {
		t.Fatalf("danh tính không xác định ⇒ 503 Retry-After 5, có %d %v", res.StatusCode, res.Header)
	}
	if hubGoi.Load() != 0 {
		t.Fatalf("hub bị gọi %d lần", hubGoi.Load())
	}
}

func TestForwardAuthAllowFromSaiKhongGoiHub(t *testing.T) {
	s, hubGoi := lapFA(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }, "10.0.0.0/8")
	if res := hoiFA(t, s, "/dashboard/x", "sid=abc"); res.StatusCode != 403 {
		t.Fatalf("%d", res.StatusCode)
	}
	if hubGoi.Load() != 0 {
		t.Fatal("không được gọi hub")
	}
}

func TestCheckForwardAuthLuat(t *testing.T) {
	cfg := func(gate, extra string) string {
		return "gate:\n" + gate + "\nhub:\n  base_url: https://hub.test\n  api_key_env: DZ_API_KEY\nidentity:\n  - {type: jwt, secret: " + strings.Repeat("s", 32) + ", issuer: i, audience: a}\nroutes:\n  - {match: \"/d/*\", action: require}\n" + extra
	}
	po := "paywall:\n  plan_code: pro\n  public_origins: [\"https://app.test\"]\n"
	ma, _, e := chayMain([]string{"--check", viet(t, cfg("  listen: \":8080\"\n  mode: forward-auth", po))}, envOK)
	if ma != 1 || !strings.Contains(e, "allow_from") {
		t.Fatalf("thiếu allow_from phải lỗi: %d %s", ma, e)
	}
	ma, _, e = chayMain([]string{"--check", viet(t, cfg("  listen: \":8080\"\n  mode: forward-auth\n  forward_auth: {allow_from: [\"không-phải-cidr\"]}", po))}, envOK)
	if ma != 1 || !strings.Contains(e, "CIDR") {
		t.Fatalf("CIDR sai: %d %s", ma, e)
	}
	ma, _, e = chayMain([]string{"--check", viet(t, cfg("  listen: \":8080\"\n  mode: forward-auth\n  forward_auth: {allow_from: [\"10.0.0.0/8\"]}", ""))}, envOK)
	if ma != 1 || !strings.Contains(e, "public_origins") {
		t.Fatalf("thiếu public_origins: %d %s", ma, e)
	}
	ma, out, e := chayMain([]string{"--check", viet(t, cfg("  listen: \":8080\"\n  mode: forward-auth\n  upstream: http://app:3000\n  forward_auth: {allow_from: [\"10.0.0.0/8\"]}", po))}, envOK)
	if ma != 0 || !strings.Contains(e, "bị bỏ qua") || !strings.Contains(out, "hợp lệ") {
		t.Fatalf("upstream thừa ⇒ cảnh báo, vẫn hợp lệ: %d %s %s", ma, out, e)
	}
}
