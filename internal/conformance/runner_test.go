package conformance

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/datazen-vn/market-gate/internal/cauhinh"
	"github.com/datazen-vn/market-gate/internal/chay"
	"github.com/datazen-vn/market-gate/internal/quyetdinh"
)

const gioMacDinh = "2026-10-13T10:00:00Z"

// dongHo giả dùng cho MỌI thứ phụ thuộc thời gian của gate (hub, JWT, đệm, paywall…); chỉ `delay_ms` của hub giả chờ thật.
type dongHo struct {
	mu sync.Mutex
	t  time.Time
}

func (d *dongHo) now() time.Time { d.mu.Lock(); defer d.mu.Unlock(); return d.t }
func (d *dongHo) tien(ms int64) {
	d.mu.Lock()
	d.t = d.t.Add(time.Duration(ms) * time.Millisecond)
	d.mu.Unlock()
}

// upstreamGia ghi lại request mà gate chuyển tiếp trong MỘT bước.
type upstreamGia struct {
	*httptest.Server
	mu     sync.Mutex
	called bool
	header http.Header
}

func (u *upstreamGia) begin() { u.mu.Lock(); u.called, u.header = false, nil; u.mu.Unlock() }

func moiUpstream(t *testing.T) *upstreamGia {
	u := &upstreamGia{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.called, u.header = true, r.Header.Clone()
		u.mu.Unlock()
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(u.Close)
	return u
}

// dungCauHinh dịch `config` của ca sang cauhinh.Config: thêm phần gate/hub.base_url mà ca không khai (không ảnh hưởng quyết định).
func dungCauHinh(t *testing.T, c Ca, upstream string) *cauhinh.Config {
	t.Helper()
	m := map[string]any{}
	for k, v := range c.Config {
		m[k] = v
	}
	m["gate"] = map[string]any{"listen": "127.0.0.1:18999", "upstream": upstream}
	h, _ := m["hub"].(map[string]any)
	hh := map[string]any{}
	for k, v := range h {
		hh[k] = v
	}
	hh["base_url"] = hubBaseURL
	m["hub"] = hh
	b, _ := json.Marshal(m)
	cfg, err := cauhinh.Doc(strings.NewReader(string(b)))
	if err != nil {
		t.Fatalf("config của ca không đọc được: %v", err)
	}
	cfg.MacDinh()
	return cfg
}

func secretCua(c Ca) string {
	if ids, ok := c.Config["identity"].([]any); ok {
		for _, i := range ids {
			if m, ok := i.(map[string]any); ok && m["type"] == "jwt" {
				if s, ok := m["secret"].(string); ok {
					return s
				}
			}
		}
	}
	return secretMacDinh
}

type chayCa struct {
	t       *testing.T
	c       Ca
	clock   *dongHo
	hub     *HubGia
	up      *upstreamGia
	handler http.Handler
	dich    *chay.Dich
	mu      sync.Mutex
	quyet   map[*http.Request]quyetdinh.KetQua
}

func chayMotCa(t *testing.T, c Ca) {
	now := gioMacDinh
	if c.Clock != nil && c.Clock.Now != "" {
		now = c.Clock.Now
	}
	t0, err := time.Parse(time.RFC3339, now)
	if err != nil {
		t.Fatalf("clock.now sai: %v", err)
	}
	r := &chayCa{t: t, c: c, clock: &dongHo{t: t0}, up: moiUpstream(t), quyet: map[*http.Request]quyetdinh.KetQua{}}
	hookURL := ""
	if ids, ok := c.Config["identity"].([]any); ok {
		for _, i := range ids {
			if m, ok := i.(map[string]any); ok && m["type"] == "session_hook" {
				hookURL, _ = m["url"].(string)
			}
		}
	}
	var jwksURL []string
	if ids, ok := c.Config["identity"].([]any); ok {
		for _, i := range ids {
			if m, ok := i.(map[string]any); ok && m["type"] == "jwt" {
				if u, ok := m["jwks_url"].(string); ok {
					jwksURL = append(jwksURL, u)
				}
			}
		}
	}
	jwksStatus := 500
	if c.JWKS != nil {
		jwksStatus = c.JWKS.Error.Status
	}
	r.hub = newHubGia(c.Hub, c.SessionHook, hookURL, jwksURL, jwksStatus)
	cfg := dungCauHinh(t, c, r.up.URL)
	r.dich, err = chay.Lap(cfg, chay.TuyChon{
		Env:  func(k string) (string, bool) { return apiKeyThu, k == "DZ_API_KEY" },
		HTTP: &http.Client{Transport: r.hub}, Now: r.clock.now, PhienBan: "conformance",
		QuanSat: func(req *http.Request, kq quyetdinh.KetQua) { r.mu.Lock(); r.quyet[req] = kq; r.mu.Unlock() },
	})
	if err != nil {
		t.Fatalf("dựng gate từ config của ca: %v", err)
	}
	r.handler = r.dich.Chinh

	for i, b := range c.Steps {
		where := c.ID + " bước " + itoa(i+1)
		switch {
		case b.ClockAdvance != nil:
			r.clock.tien(*b.ClockAdvance)
		case b.Invalidate != nil:
			r.dich.Cong.Khach.Huy(*b.Invalidate)
			r.dich.Cong.Cong.Paywall.Quen(*b.Invalidate)
		case b.Parallel != nil:
			r.chaySong(where, *b.Parallel)
		case b.Request != nil && b.Expect != nil:
			r.chayMot(where, *b.Request, *b.Expect)
		default:
			t.Fatalf("%s: bước không hiểu được", where)
		}
	}
	if len(r.hub.loiCauHinh) > 0 {
		t.Fatalf("%s: lỗi cấu hình ca/runner: %v", c.ID, r.hub.loiCauHinh)
	}
	// README #5: hub KHÔNG BAO GIỜ thấy header của khách (Cookie/Authorization của khách); chỉ khoá API + User-Agent (+ If-None-Match).
	for _, g := range r.hub.calls {
		if g.header.Get("Authorization") != "Bearer "+apiKeyThu {
			t.Errorf("%s: Authorization tới hub = %q", c.ID, g.header.Get("Authorization"))
		}
		if g.header.Get("Cookie") != "" {
			t.Errorf("%s: cookie của khách lọt sang hub", c.ID)
		}
	}
}

func itoa(i int) string {
	b, _ := json.Marshal(i)
	return string(b)
}

// gui dựng request THÔ (RequestURI nguyên văn như trong ca — không để net/url chuẩn hoá `..`/`%2e`) rồi chạy qua handler.
func (r *chayCa) gui(y YeuCau) (*httptest.ResponseRecorder, *http.Request) {
	var body io.Reader
	if y.Body != "" && y.Method != "GET" && y.Method != "HEAD" {
		body = strings.NewReader(y.Body)
	}
	req := httptest.NewRequest(y.Method, y.Path, body)
	host := y.Host
	if host == "" {
		host = r.publicHost()
	}
	req.Host = host
	req.RemoteAddr = "192.0.2.10:40000"
	secret := secretCua(r.c)
	for k, v := range y.Headers {
		ev, err := mrongJWT(v, secret, r.clock.now())
		if err != nil {
			r.t.Fatalf("%s: %v", r.c.ID, err)
		}
		req.Header.Set(k, ev)
	}
	rec := httptest.NewRecorder()
	r.handler.ServeHTTP(rec, req)
	return rec, req
}

func (r *chayCa) publicHost() string {
	pw, _ := r.c.Config["paywall"].(map[string]any)
	po, _ := pw["public_origins"].([]any)
	if len(po) == 0 {
		return "app.test"
	}
	s, _ := po[0].(string)
	return strings.TrimPrefix(s, "https://")
}
