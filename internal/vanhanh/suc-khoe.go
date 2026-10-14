package vanhanh

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/datazen-vn/market-gate/internal/hub"
)

const (
	tuoiKetQuaSanSang = 5 * time.Second
	hanKiemSanSang    = 3 * time.Second
)

// Kiem là một điều kiện "sẵn sàng" có tên (vd upstream TCP, hub + khoá API).
type Kiem struct {
	Ten string
	Fn  func(ctx context.Context) error
}

// KiemTCP: kết nối TCP tới `diaChi` (host:port) được không.
func KiemTCP(diaChi string) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		c, err := (&net.Dialer{}).DialContext(ctx, "tcp", diaChi)
		if err != nil {
			return err
		}
		return c.Close()
	}
}

// SanSang chạy các Kiem và nhớ kết quả 5 giây: cổng vận hành bind riêng nhưng readyz vẫn không được thành nút "đập hub".
type SanSang struct {
	cacKiem []Kiem
	now     func() time.Time
	mu      sync.Mutex
	luc     time.Time
	ketQua  map[string]string
	ok      bool
}

// MoiSanSang tạo bộ kiểm; `now` nil ⇒ time.Now.
func MoiSanSang(now func() time.Time, cacKiem ...Kiem) *SanSang {
	if now == nil {
		now = time.Now
	}
	return &SanSang{cacKiem: cacKiem, now: now}
}

// Chay trả (sẵn sàng, kết quả theo tên). Kết quả chỉ chứa "ok" hoặc MÃ lỗi ổn định (dz_…/"loi"), KHÔNG chứa thông báo lỗi mạng
// (có thể lộ địa chỉ nội bộ).
func (s *SanSang) Chay(ctx context.Context) (bool, map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ketQua != nil && s.now().Sub(s.luc) < tuoiKetQuaSanSang {
		return s.ok, s.ketQua
	}
	kq := map[string]string{}
	ok := true
	for _, k := range s.cacKiem {
		c, huy := context.WithTimeout(ctx, hanKiemSanSang)
		err := k.Fn(c)
		huy()
		switch {
		case err == nil:
			kq[k.Ten] = "ok"
		default:
			ok = false
			kq[k.Ten] = "loi"
			if l, la := hub.ALoi(err); la {
				kq[k.Ten] = l.Ma
			}
		}
	}
	s.luc, s.ketQua, s.ok = s.now(), kq, ok
	return ok, kq
}

// Ops là handler của cổng vận hành: GET/HEAD `/_dz/healthz` (tiến trình sống), `/_dz/readyz` (upstream TCP + hub/khoá API),
// `/_dz/metrics` (Prometheus). Mọi đường khác 404. KHÔNG bind chung với cổng của khách.
func Ops(do *DoLuong, ss *SanSang) http.Handler {
	mux := http.NewServeMux()
	chiGET := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			h(w, r)
		}
	}
	mux.HandleFunc("/_dz/healthz", chiGET(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	}))
	mux.HandleFunc("/_dz/readyz", chiGET(func(w http.ResponseWriter, r *http.Request) {
		ok, kq := ss.Chay(r.Context())
		ten := make([]string, 0, len(kq))
		for k := range kq {
			ten = append(ten, k)
		}
		sort.Strings(ten)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"ready": ok, "checks": kq})
	}))
	mux.HandleFunc("/_dz/metrics", chiGET(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		do.Viet(w)
	}))
	return mux
}
