package chay

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/datazen-vn/datazen-gate/internal/cauhinh"
	"github.com/datazen-vn/datazen-gate/internal/quyetdinh"
)

// chayLogPath dựng gate thật với ops.log_path = mode, gửi ba request (tuyến khớp / không khớp / bypass, đều có query chứa bí mật),
// trả dòng log "request" theo thứ tự cùng toàn bộ log thô và metrics.
func chayLogPath(t *testing.T, mode string) (dong []map[string]any, thô, metrics string) {
	t.Helper()
	hubGia := httptest.NewServer(http.NotFoundHandler())
	defer hubGia.Close()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("app")) }))
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
	if mode != "" {
		cfg.Ops.LogPath = mode
	}
	cfg.Identity = []cauhinh.Identity{{Type: "jwt", Secret: strings.Repeat("s", 32), Issuer: "i", Audience: "a"}}
	var buf bytes.Buffer
	xong := make(chan struct{}, 8) // QuanSat chạy SAU khi ghi log ⇒ nhận tín hiệu là log đã có (không chờ theo thời gian)
	d, err := Lap(cfg, TuyChon{
		Env: envOK, HTTP: hubGia.Client(), PhienBan: "9.9.9",
		Log:     slog.New(slog.NewJSONHandler(&buf, nil)),
		QuanSat: func(*http.Request, quyetdinh.KetQua) { xong <- struct{}{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(d.Chinh)
	defer s.Close()
	for _, p := range []string{
		"/dashboard/nguoi@mail.vn?token=BIMAT",
		"/don-hang/DH-QZXK?token=BIMAT",
		"/api/dz/hook/abc?token=BIMAT",
	} {
		res, err := http.Get(s.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		<-xong
	}
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("log không phải JSON: %q", l)
		}
		if m["msg"] == "request" {
			dong = append(dong, m)
		}
	}
	if len(dong) != 3 {
		t.Fatalf("cần 3 dòng request, có %d:\n%s", len(dong), buf.String())
	}
	var m bytes.Buffer
	d.Do.Viet(&m)
	return dong, buf.String(), m.String()
}

func TestLogPathBaCheDo(t *testing.T) {
	cacCa := []struct {
		mode      string
		mong      []string // giá trị path theo thứ tự request; nil = không có trường path
		khongChua []string // không được xuất hiện ở bất kỳ đâu trong log
	}{
		{"", []string{"/dashboard/*", "unmatched", "bypass"}, []string{"BIMAT", "nguoi@mail.vn", "DH-QZXK", "?"}}, // mặc định = route
		{"route", []string{"/dashboard/*", "unmatched", "bypass"}, []string{"BIMAT", "nguoi@mail.vn", "DH-QZXK", "?"}},
		{"full", []string{"/dashboard/nguoi@mail.vn", "/don-hang/DH-QZXK", "/api/dz/hook/abc"}, []string{"BIMAT", "?"}},
		{"off", nil, []string{"BIMAT", "nguoi@mail.vn", "DH-QZXK", "/don-hang", "/api/dz", "dashboard", "bypass", "unmatched"}},
	}
	for _, c := range cacCa {
		t.Run("mode="+c.mode, func(t *testing.T) {
			dong, tho, metrics := chayLogPath(t, c.mode)
			for i, m := range dong {
				got, co := m["path"]
				if c.mong == nil {
					if co {
						t.Fatalf("off: không được có trường path, có %v", got)
					}
					continue
				}
				if got != c.mong[i] {
					t.Fatalf("request %d: path = %v, muốn %q", i, got, c.mong[i])
				}
			}
			for _, x := range c.khongChua {
				if strings.Contains(tho, x) {
					t.Fatalf("log chứa %q:\n%s", x, tho)
				}
			}
			// metric không bao giờ dùng đường dẫn làm nhãn
			for _, x := range []string{"BIMAT", "nguoi@mail.vn", "DH-QZXK", "don-hang", "path="} {
				if strings.Contains(metrics, x) {
					t.Fatalf("metric chứa %q:\n%s", x, metrics)
				}
			}
		})
	}
}

// Request vòng lặp (508) bị chặn trước khi xét tuyến ⇒ path là "loop", không phải "unmatched", và không lộ đường dẫn.
func TestLogPathVongLapGhiLoop(t *testing.T) {
	hubGia := httptest.NewServer(http.NotFoundHandler())
	defer hubGia.Close()
	up := httptest.NewServer(http.NotFoundHandler())
	defer up.Close()
	cfg, err := cauhinh.Doc(strings.NewReader(`{"gate": {"listen": ":18080", "upstream": "` + up.URL + `"}, "hub": {"base_url": "` + hubGia.URL + `"}, "routes": [{"match": "/dashboard/*", "action": "require"}], "paywall": {"plan_code": "pro", "public_origins": ["https://app.test"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	cfg.MacDinh()
	cfg.Identity = []cauhinh.Identity{{Type: "jwt", Secret: strings.Repeat("s", 32), Issuer: "i", Audience: "a"}}
	var buf bytes.Buffer
	xong := make(chan struct{}, 1)
	d, err := Lap(cfg, TuyChon{Env: envOK, HTTP: hubGia.Client(), PhienBan: "t", Log: slog.New(slog.NewJSONHandler(&buf, nil)),
		QuanSat: func(*http.Request, quyetdinh.KetQua) { xong <- struct{}{} }})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(d.Chinh)
	defer s.Close()
	req, _ := http.NewRequest("GET", s.URL+"/don-hang/DH-QZXK?token=BIMAT", nil)
	req.Header.Set("X-DZ-Gate-Hop", "1")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	<-xong
	if res.StatusCode != 508 || !strings.Contains(buf.String(), `"path":"loop"`) || strings.Contains(buf.String(), "DH-QZXK") || strings.Contains(buf.String(), "BIMAT") {
		t.Fatalf("508=%d log:\n%s", res.StatusCode, buf.String())
	}
}
