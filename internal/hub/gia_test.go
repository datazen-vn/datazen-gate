package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

var goc = time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)

// dongHo giả, an toàn cho nhiều goroutine.
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

// luotGoi là một lời gọi tới hub giả.
type luotGoi struct {
	phuongThuc, duongDanTho string
	header                  http.Header
	than                    string
}

// hubGia: hub giả; `xuLy` quyết định trả lời cho lần gọi thứ n (từ 1), tính riêng cho mỗi đường dẫn thô.
type hubGia struct {
	*httptest.Server
	mu   sync.Mutex
	goi  []luotGoi
	xuLy func(n int, r *http.Request, than string, w http.ResponseWriter)
	tong int
}

func moiHubGia(t *testing.T, xuLy func(n int, r *http.Request, than string, w http.ResponseWriter)) *hubGia {
	t.Helper()
	h := &hubGia{xuLy: xuLy}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		h.mu.Lock()
		h.tong++
		n := h.tong
		h.goi = append(h.goi, luotGoi{r.Method, r.URL.EscapedPath(), r.Header.Clone(), string(b)})
		h.mu.Unlock()
		h.xuLy(n, r, string(b), w)
	}))
	t.Cleanup(h.Close)
	return h
}

func (h *hubGia) soGoi() int { h.mu.Lock(); defer h.mu.Unlock(); return len(h.goi) }
func (h *hubGia) cuoi() luotGoi {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.goi[len(h.goi)-1]
}

// snap dựng thân snapshot; `kem` ghi đè/thêm trường.
func snap(ma, trangThai string, kem map[string]any) map[string]any {
	m := map[string]any{
		"customer_ref": ma, "status": trangThai, "plan_code": nil, "features": map[string]bool{},
		"limits": map[string]any{}, "current_period_end": nil, "grace_until": nil, "version": 1,
	}
	for k, v := range kem {
		m[k] = v
	}
	return m
}

func traSnap(w http.ResponseWriter, body map[string]any, etag string) {
	if etag != "" {
		w.Header().Set("ETag", etag)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func traLoi(w http.ResponseWriter, tt int, ma string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(tt)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": ma, "message": "x", "trace_id": "trace-abc12345"})
}

func khachThu(h *hubGia, dh *dongHo, sua func(*CauHinh)) *Khach {
	cfg := CauHinh{
		BaseURL: h.URL, APIKey: "dz_test_khoa", Timeout: 2 * time.Second, TTL: 60 * time.Second,
		HTTP: h.Client(), Now: dh.now, UserAgent: "dz-gate/test",
	}
	if sua != nil {
		sua(&cfg)
	}
	return Moi(cfg)
}
