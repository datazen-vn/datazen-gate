package quyetdinh

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datazen-vn/datazen-gate/internal/danhtinh"
	"github.com/datazen-vn/datazen-gate/internal/hub"
	"github.com/datazen-vn/datazen-gate/internal/tuyen"
)

var goc = time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)

type dongHo struct {
	mu sync.Mutex
	t  time.Time
}

func (d *dongHo) now() time.Time { d.mu.Lock(); defer d.mu.Unlock(); return d.t }
func (d *dongHo) tien(x time.Duration) {
	d.mu.Lock()
	d.t = d.t.Add(x)
	d.mu.Unlock()
}

// theo: danh tính = header `x-user` (rỗng = chưa đăng nhập); `x-user-hong` ⇒ lỗi hạ tầng.
type theoHeader struct{}

func (theoHeader) NhanDien(_ context.Context, r *http.Request) (string, error) {
	if r.Header.Get("X-User-Hong") != "" {
		return "", &danhtinh.LoiHaTang{ChiTiet: "hook chết"}
	}
	return r.Header.Get("X-User"), nil
}

type luotGoi struct{ phuongThuc, duong, than string }

// gioi: hub giả cho một khách; `xuLyKhach(n)` trả snapshot theo lần gọi entitlements thứ n (từ 1).
type thietLap struct {
	t        *testing.T
	dh       *dongHo
	hub      *httptest.Server
	cong     *Cong
	mu       sync.Mutex
	goi      []luotGoi
	soEnt    int
	khach    func(n int) (int, map[string]any) // (HTTP status, thân)
	phien    func(n int) (int, map[string]any) // POST /v1/checkout-sessions
	docPhien func(id string) (int, map[string]any)
	loi      []error
}

func snapGia(ma, tt string, kem map[string]any) map[string]any {
	m := map[string]any{"customer_ref": ma, "status": tt, "plan_code": nil, "features": map[string]bool{}, "limits": map[string]any{}, "current_period_end": nil, "grace_until": nil, "version": 1}
	for k, v := range kem {
		m[k] = v
	}
	return m
}

func hetHanSau(dh *dongHo, d time.Duration) string { return dh.now().Add(d).Format(time.RFC3339) }

func (s *thietLap) entGoi() int { s.mu.Lock(); defer s.mu.Unlock(); return s.soEnt }
func (s *thietLap) soPOST() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, g := range s.goi {
		if g.phuongThuc == "POST" {
			n++
		}
	}
	return n
}
func (s *thietLap) thanPOSTCuoi() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := len(s.goi) - 1; i >= 0; i-- {
		if s.goi[i].phuongThuc == "POST" {
			var m map[string]string
			_ = json.Unmarshal([]byte(s.goi[i].than), &m)
			return m
		}
	}
	return nil
}

func (s *thietLap) tra(w http.ResponseWriter, tt int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(tt)
	_ = json.NewEncoder(w).Encode(body)
}

type tuyChon struct {
	routes   []tuyen.Dong
	macDinh  string
	cheDoLoi string
	bo       []danhtinh.Bo
	paywall  *CauHinhPaywall
}

func moiThietLap(t *testing.T, tc tuyChon) *thietLap {
	t.Helper()
	s := &thietLap{t: t, dh: &dongHo{t: goc}}
	s.khach = func(n int) (int, map[string]any) { return 200, snapGia("cus_1", "none", nil) }
	s.phien = func(n int) (int, map[string]any) {
		return 201, map[string]any{"id": "11111111-1111-4111-8111-111111111111", "url": "https://pay.test/c/abc", "expires_at": hetHanSau(s.dh, time.Hour), "replace_after": hetHanSau(s.dh, 50*time.Minute)}
	}
	s.docPhien = func(string) (int, map[string]any) { return 404, map[string]any{"code": "phien-khong-ton-tai"} }
	s.hub = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		s.mu.Lock()
		s.goi = append(s.goi, luotGoi{r.Method, r.URL.EscapedPath(), string(b)})
		var n int
		nGoi := len(s.goi)
		laEnt := strings.HasSuffix(r.URL.Path, "/entitlements")
		if laEnt {
			s.soEnt++
			n = s.soEnt
		}
		s.mu.Unlock()
		switch {
		case laEnt:
			tt, body := s.khach(n)
			s.tra(w, tt, body)
		case r.Method == "POST":
			tt, body := s.phien(nGoi)
			s.tra(w, tt, body)
		default:
			id := strings.TrimPrefix(r.URL.Path, "/v1/checkout-sessions/")
			tt, body := s.docPhien(id)
			s.tra(w, tt, body)
		}
	}))
	t.Cleanup(s.hub.Close)

	khach := hub.Moi(hub.CauHinh{BaseURL: s.hub.URL, APIKey: "dz_test_khoa", TTL: 60 * time.Second, HTTP: s.hub.Client(), Now: s.dh.now, Timeout: 2 * time.Second})
	cfgPw := CauHinhPaywall{MaGoi: "pro", PublicOrigins: []string{"https://app.test"}, LoginURL: "/login", SuccessPath: "/billing/return", CancelPath: "/pricing"}
	if tc.paywall != nil {
		cfgPw = *tc.paywall
	}
	pw, err := MoiPaywall(cfgPw, khach, s.dh.now)
	if err != nil {
		t.Fatal(err)
	}
	routes := tc.routes
	if routes == nil {
		routes = []tuyen.Dong{
			{Khop: "/api/public/*", HanhDong: tuyen.Cho},
			{Khop: "/premium/*", HanhDong: tuyen.Can, TinhNang: "premium"},
			{Khop: "/api/premium/*", HanhDong: tuyen.Can, TinhNang: "export", CheDoLoi: "open"},
			{Khop: "/pricing", HanhDong: tuyen.Mem},
		}
	}
	bypass := append(append([]string{}, tuyen.BypassMacDinh...), pw.Bypass...)
	bo := tc.bo
	if bo == nil {
		bo = []danhtinh.Bo{theoHeader{}}
	}
	s.cong = &Cong{Quyen: khach, DanhTinh: bo, Bang: tuyen.TaoBang(bypass, routes, tc.macDinh), Paywall: pw, CheDoLoi: tc.cheDoLoi, BaoLoi: func(err error) {
		s.mu.Lock()
		s.loi = append(s.loi, err)
		s.mu.Unlock()
	}}
	return s
}

type yc struct {
	phuongThuc, uri string
	header          map[string]string
	anon            bool
}

var (
	api       = map[string]string{"Accept": "application/json"}
	dieuHuong = map[string]string{"Accept": "text/html", "Sec-Fetch-Mode": "navigate"}
)

func (s *thietLap) xuLy(y yc) KetQua {
	s.t.Helper()
	if y.phuongThuc == "" {
		y.phuongThuc = "GET"
	}
	r := httptest.NewRequest(y.phuongThuc, y.uri, nil)
	r.Host = "app.test"
	for k, v := range y.header {
		r.Header.Set(k, v)
	}
	if !y.anon && r.Header.Get("X-User") == "" {
		r.Header.Set("X-User", "cus_1")
	}
	return s.cong.XuLy(context.Background(), r)
}

func (s *thietLap) soLoi() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.loi) }

// thanJSON đọc thân JSON của phản hồi gate.
func thanJSON(t *testing.T, p *PhanHoi) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(p.Than, &m); err != nil {
		t.Fatalf("thân không phải JSON: %q (%v)", p.Than, err)
	}
	return m
}
