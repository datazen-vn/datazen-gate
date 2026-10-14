package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func cong(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}

// TestBinaryThat: build binary thật, chạy với cấu hình thật, đi một vòng qua cổng + cổng vận hành, tắt êm bằng SIGTERM (mã 0).
func TestBinaryThat(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "dz-gate")
	if out, err := exec.Command("go", "build", "-ldflags", "-X main.phienBan=1.2.3", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	if out, err := exec.Command(bin, "--version").Output(); err != nil || string(out) != "dz-gate 1.2.3\n" {
		t.Fatalf("--version: %q %v", out, err)
	}

	hubGia := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ma := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/customers/"), "/entitlements")
		_ = json.NewEncoder(w).Encode(map[string]any{"customer_ref": ma, "status": "none", "features": map[string]bool{}})
	}))
	defer hubGia.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "app:%s:%s", r.Header.Get("X-DZ-Gate-Hop"), r.Header.Get("X-DZ-Customer-Ref"))
	}))
	defer up.Close()

	gateAddr, opsAddr := cong(t), cong(t)
	cfg := fmt.Sprintf(`
gate: {listen: "%s", upstream: "%s"}
hub: {base_url: "%s", cache_ttl: 30s}
identity:
  - {type: jwt, secret_env: DZ_JWT_SECRET, issuer: i, audience: a}
routes:
  - {match: "/dashboard/*", action: require}
paywall: {plan_code: pro, public_origins: ["https://app.test"]}
ops: {listen: "%s"}
`, gateAddr, up.URL, hubGia.URL, opsAddr)
	cfgPath := filepath.Join(dir, "gate.yaml")
	if err := os.WriteFile(cfgPath, []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "DZ_API_KEY=dz_test_khoa-abc123", "DZ_JWT_SECRET="+strings.Repeat("s", 32))

	chk := exec.Command(bin, "--check", cfgPath)
	chk.Env = env
	if out, err := chk.CombinedOutput(); err != nil {
		t.Fatalf("--check: %v\n%s", err, out)
	}

	var stderr bytes.Buffer
	cmd := exec.Command(bin, "--config", cfgPath)
	cmd.Env, cmd.Stderr = env, &stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	daTat := make(chan error, 1)
	go func() { daTat <- cmd.Wait() }()
	defer func() { _ = cmd.Process.Kill() }()

	han := time.Now().Add(10 * time.Second)
	for {
		if r, err := http.Get("http://" + opsAddr + "/_dz/readyz"); err == nil {
			r.Body.Close()
			if r.StatusCode == 200 {
				break
			}
		}
		if time.Now().After(han) {
			t.Fatalf("readyz không lên:\n%s", stderr.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if res, err := http.Get("http://" + gateAddr + "/dashboard/x"); err != nil || res.StatusCode != 401 {
		t.Fatalf("chưa đăng nhập phải 401: %v %v", res, err)
	}
	res, err := http.Get("http://" + gateAddr + "/mo")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	_, _ = b.ReadFrom(res.Body)
	res.Body.Close()
	if b.String() != "app:1:" {
		t.Fatalf("tuyến allow phải tới upstream kèm hop header: %q", b.String())
	}
	m, _ := http.Get("http://" + opsAddr + "/_dz/metrics")
	var mb bytes.Buffer
	_, _ = mb.ReadFrom(m.Body)
	m.Body.Close()
	if !strings.Contains(mb.String(), `dz_gate_requests_total{ket_qua="no_identity"} 1`) {
		t.Fatalf("metric: %s", mb.String())
	}
	hc := exec.Command(bin, "--healthcheck", "--config", cfgPath)
	if out, err := hc.CombinedOutput(); err != nil {
		t.Fatalf("--healthcheck: %v\n%s", err, out)
	}

	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case err := <-daTat:
		if err != nil {
			t.Fatalf("tắt êm phải mã 0: %v\n%s", err, stderr.String())
		}
	case <-time.After(15 * time.Second):
		t.Fatal("không tắt trong 15 s sau SIGTERM")
	}
	if strings.Contains(stderr.String(), "dz_test_khoa") || strings.Contains(stderr.String(), "ssss") {
		t.Fatal("log không được chứa khoá/secret")
	}
}
