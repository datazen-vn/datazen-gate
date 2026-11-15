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

// chayLogPathForwardAuth dựng gate forward-auth thật với ops.log_path = mode và allow_from = allowFrom, gửi ba lời hỏi của proxy
// (tuyến allow / tuyến require chưa đăng nhập / uri sai), mỗi lời hỏi có query chứa bí mật. Trả dòng log "request" theo thứ tự + log thô.
func chayLogPathForwardAuth(t *testing.T, mode, allowFrom string) (dong []map[string]any, tho string) {
	t.Helper()
	hubGia := httptest.NewServer(http.NotFoundHandler())
	defer hubGia.Close()
	cfg, err := cauhinh.Doc(strings.NewReader(`{
	  "gate": {"listen": ":18083", "mode": "forward-auth", "forward_auth": {"allow_from": ["` + allowFrom + `"]}},
	  "hub": {"base_url": "` + hubGia.URL + `"},
	  "routes": [{"match": "/api/public/*", "action": "allow"}, {"match": "/dashboard/*", "action": "require", "feature": "dashboard"}],
	  "paywall": {"plan_code": "pro", "public_origins": ["https://app.test"]}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	cfg.MacDinh()
	cfg.Ops.LogPath = mode
	cfg.Identity = []cauhinh.Identity{{Type: "jwt", Secret: strings.Repeat("s", 32), Issuer: "i", Audience: "a"}}
	var buf bytes.Buffer
	xong := make(chan struct{}, 8) // QuanSat chạy SAU khi ghi log ⇒ nhận tín hiệu là log đã có
	d, err := Lap(cfg, TuyChon{
		Env: envOK, HTTP: hubGia.Client(), PhienBan: "t",
		Log:     slog.New(slog.NewJSONHandler(&buf, nil)),
		QuanSat: func(*http.Request, quyetdinh.KetQua) { xong <- struct{}{} },
	})
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(d.Chinh)
	defer s.Close()
	for _, uri := range []string{
		"/api/public/ban-do?token=BIMAT",
		"/dashboard/nguoi@mail.vn?token=BIMAT",
		"don-hang/DH-QZXK?token=BIMAT", // thiếu "/" đầu ⇒ request ảo hỏng
	} {
		req, _ := http.NewRequest("GET", s.URL+"/_dz/auth", nil)
		req.Header.Set("X-Forwarded-Uri", uri)
		req.Header.Set("X-Forwarded-Host", "app.test")
		res, err := http.DefaultTransport.RoundTrip(req)
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
	return dong, buf.String()
}

// forward-auth đi chung đường log với chế độ proxy: route chỉ ghi nhãn hữu hạn ("forward" cho lời hỏi bị từ chối trước khi xét tuyến),
// full bỏ query, off bỏ trường path; không chế độ nào để lọt query/đường dẫn thật của request ảo vào ngoài full.
func TestLogPathForwardAuth(t *testing.T) {
	cacCa := []struct {
		mode, allowFrom string
		mong            []string // nil = không có trường path
		khongChua       []string
	}{
		{"route", "127.0.0.0/8", []string{"/api/public/*", "/dashboard/*", "forward"}, []string{"BIMAT", "nguoi@mail.vn", "ban-do", "DH-QZXK", "?"}},
		{"full", "127.0.0.0/8", []string{"/api/public/ban-do", "/dashboard/nguoi@mail.vn", "/_dz/auth"}, []string{"BIMAT", "DH-QZXK", "?"}},
		{"off", "127.0.0.0/8", nil, []string{"BIMAT", "nguoi@mail.vn", "ban-do", "DH-QZXK", "dashboard", `"path"`}},
		// nguồn không tin ⇒ không đọc header proxy, chưa xét tuyến: nhãn "forward", không lộ gì của request ảo
		{"route", "10.0.0.0/8", []string{"forward", "forward", "forward"}, []string{"BIMAT", "nguoi@mail.vn", "ban-do", "DH-QZXK", "unmatched"}},
	}
	for _, c := range cacCa {
		t.Run(c.mode+"/"+c.allowFrom, func(t *testing.T) {
			dong, tho := chayLogPathForwardAuth(t, c.mode, c.allowFrom)
			for i, m := range dong {
				got, co := m["path"]
				if c.mong == nil {
					if co {
						t.Fatalf("off: không được có trường path, có %v", got)
					}
					continue
				}
				if got != c.mong[i] {
					t.Fatalf("lời hỏi %d: path = %v, muốn %q\n%s", i, got, c.mong[i], tho)
				}
			}
			for _, x := range c.khongChua {
				if strings.Contains(tho, x) {
					t.Fatalf("log chứa %q:\n%s", x, tho)
				}
			}
		})
	}
}
