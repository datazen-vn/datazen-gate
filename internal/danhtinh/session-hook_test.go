package danhtinh

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type hookGia struct {
	*httptest.Server
	mu  sync.Mutex
	goi []http.Header
	n   atomic.Int32
}

func moiHookGia(t *testing.T, xuLy func(r *http.Request, w http.ResponseWriter)) *hookGia {
	h := &hookGia{}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.n.Add(1)
		h.mu.Lock()
		h.goi = append(h.goi, r.Header.Clone())
		h.mu.Unlock()
		xuLy(r, w)
	}))
	t.Cleanup(h.Close)
	return h
}

// theoCookie: cookie "sid=ok" ⇒ 200 {"user":{"id":"cus_1"}}; "sid=so" ⇒ id số; "sid=het" ⇒ 401; còn lại 403.
func theoCookie(r *http.Request, w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Header.Get("Cookie") {
	case "sid=ok":
		_, _ = w.Write([]byte(`{"user":{"id":"cus_1","email":"a@b.vn"}}`))
	case "sid=so":
		_, _ = w.Write([]byte(`{"user":{"id":12345}}`))
	case "sid=het":
		w.WriteHeader(401)
	default:
		w.WriteHeader(403)
	}
}

func yeuCauCookie(cookie string) *http.Request {
	r, _ := http.NewRequest("GET", "http://app.test/x", nil)
	if cookie != "" {
		r.Header.Set("Cookie", cookie)
	}
	return r
}

func hookThu(t *testing.T, h *hookGia, sua func(*CauHinhSessionHook)) *SessionHook {
	cfg := CauHinhSessionHook{URL: h.URL + "/api/me", HTTP: h.Client(), AllowHosts: nil}
	if sua != nil {
		sua(&cfg)
	}
	s, err := MoiSessionHook(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestSessionHookDaDangNhapVaChuaDangNhap(t *testing.T) {
	h := moiHookGia(t, theoCookie)
	s := hookThu(t, h, nil)
	cacTruongHop := []struct {
		cookie string
		mong   string
	}{{"sid=ok", "cus_1"}, {"sid=so", "12345"}, {"sid=het", ""}, {"sid=la", ""}}
	for _, c := range cacTruongHop {
		ma, err := s.NhanDien(context.Background(), yeuCauCookie(c.cookie))
		if err != nil || ma != c.mong {
			t.Errorf("%s: (%q, %v), mong %q", c.cookie, ma, err, c.mong)
		}
	}
}

func TestSessionHookKhongCoHeaderThiKhongGoiHook(t *testing.T) {
	h := moiHookGia(t, theoCookie)
	s := hookThu(t, h, nil)
	if ma, err := s.NhanDien(context.Background(), yeuCauCookie("")); ma != "" || err != nil {
		t.Fatalf("(%q, %v)", ma, err)
	}
	if h.n.Load() != 0 {
		t.Fatal("người lạ không mang cookie/authorization không được khuếch đại thành lời gọi hook")
	}
}

func TestSessionHookDemChiKhiDaDangNhapVaKhoaBangBam(t *testing.T) {
	dh := &dongHoJWKS{t: time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)}
	h := moiHookGia(t, theoCookie)
	s := hookThu(t, h, func(c *CauHinhSessionHook) { c.Now = dh.now })
	for i := 0; i < 5; i++ {
		_, _ = s.NhanDien(context.Background(), yeuCauCookie("sid=ok"))
	}
	if h.n.Load() != 1 {
		t.Fatalf("đã đăng nhập được đệm: %d lời gọi", h.n.Load())
	}
	dh.tien(31 * time.Second) // quá 30 s mặc định
	_, _ = s.NhanDien(context.Background(), yeuCauCookie("sid=ok"))
	if h.n.Load() != 2 {
		t.Fatalf("hết TTL phải hỏi lại: %d", h.n.Load())
	}
	// "Chưa đăng nhập" KHÔNG đệm: đăng nhập xong có hiệu lực ngay.
	for i := 0; i < 3; i++ {
		_, _ = s.NhanDien(context.Background(), yeuCauCookie("sid=het"))
	}
	if h.n.Load() != 5 {
		t.Fatalf("401 không được đệm: %d", h.n.Load())
	}
	// Khoá đệm là SHA-256 của header chuyển tiếp, không giữ cookie thô.
	if _, ok := s.dem.Lay("sid=ok"); ok {
		t.Fatal("khoá đệm không được là cookie thô")
	}
	khoa := bam([]capHeader{{"cookie", "sid=ok"}})
	if _, ok := s.dem.Lay(khoa); !ok || len(khoa) != 64 || strings.Contains(khoa, "sid") {
		t.Fatalf("khoá đệm phải là SHA-256 hex: %q", khoa)
	}
}

func TestSessionHookTatDem(t *testing.T) {
	h := moiHookGia(t, theoCookie)
	s := hookThu(t, h, func(c *CauHinhSessionHook) { c.TatDem = true })
	_, _ = s.NhanDien(context.Background(), yeuCauCookie("sid=ok"))
	_, _ = s.NhanDien(context.Background(), yeuCauCookie("sid=ok"))
	if h.n.Load() != 2 {
		t.Fatalf("tắt đệm: %d", h.n.Load())
	}
}

func TestSessionHookGopDongThoiCungPhien(t *testing.T) {
	thaDi := make(chan struct{})
	h := moiHookGia(t, func(r *http.Request, w http.ResponseWriter) {
		<-thaDi
		theoCookie(r, w)
	})
	s := hookThu(t, h, nil)
	var wg sync.WaitGroup
	for i := 0; i < 15; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ma, err := s.NhanDien(context.Background(), yeuCauCookie("sid=ok")); err != nil || ma != "cus_1" {
				t.Errorf("(%q, %v)", ma, err)
			}
		}()
	}
	for h.n.Load() < 1 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	close(thaDi)
	wg.Wait()
	if h.n.Load() != 1 {
		t.Fatalf("15 lời gọi cùng phiên gộp thành 1: %d", h.n.Load())
	}
}

func TestSessionHookLoiHaTangKhongPhaiChuaDangNhap(t *testing.T) {
	cacTruongHop := map[string]func(r *http.Request, w http.ResponseWriter){
		"500":             func(r *http.Request, w http.ResponseWriter) { w.WriteHeader(500) },
		"302":             func(r *http.Request, w http.ResponseWriter) { http.Redirect(w, r, "https://evil.test/", 302) },
		"204 rỗng":        func(r *http.Request, w http.ResponseWriter) { w.WriteHeader(204) },
		"không phải JSON": func(r *http.Request, w http.ResponseWriter) { _, _ = w.Write([]byte("<html>")) },
		"id là object":    func(r *http.Request, w http.ResponseWriter) { _, _ = w.Write([]byte(`{"user":{"id":{"x":1}}}`)) },
		"id là bool":      func(r *http.Request, w http.ResponseWriter) { _, _ = w.Write([]byte(`{"user":{"id":true}}`)) },
		"id số thập phân": func(r *http.Request, w http.ResponseWriter) { _, _ = w.Write([]byte(`{"user":{"id":1.5}}`)) },
		"thân quá 256 KB": func(r *http.Request, w http.ResponseWriter) {
			_, _ = w.Write([]byte(`{"user":{"id":"x"},"pad":"` + strings.Repeat("x", 300<<10) + `"}`))
		},
	}
	for ten, xuLy := range cacTruongHop {
		h := moiHookGia(t, xuLy)
		_, err := hookThu(t, h, nil).NhanDien(context.Background(), yeuCauCookie("sid=ok"))
		var ha *LoiHaTang
		if !errors.As(err, &ha) {
			t.Errorf("%s: mong *LoiHaTang, có %v", ten, err)
		}
	}
}

func TestSessionHookIDVangHoacNullLaChuaDangNhap(t *testing.T) {
	for _, than := range []string{`{}`, `{"user":null}`, `{"user":{}}`, `{"user":{"id":null}}`, `{"user":{"id":""}}`, `{"user":[1]}`, `{"user":"x"}`} {
		h := moiHookGia(t, func(r *http.Request, w http.ResponseWriter) { _, _ = w.Write([]byte(than)) })
		ma, err := hookThu(t, h, nil).NhanDien(context.Background(), yeuCauCookie("sid=ok"))
		if ma != "" || err != nil {
			t.Errorf("%s: (%q, %v) mong chưa đăng nhập", than, ma, err)
		}
	}
}

func TestSessionHookTimeout(t *testing.T) {
	h := moiHookGia(t, func(r *http.Request, w http.ResponseWriter) { time.Sleep(300 * time.Millisecond) })
	s := hookThu(t, h, func(c *CauHinhSessionHook) { c.Timeout = 50 * time.Millisecond })
	_, err := s.NhanDien(context.Background(), yeuCauCookie("sid=ok"))
	var ha *LoiHaTang
	if !errors.As(err, &ha) {
		t.Fatalf("quá hạn ⇒ LoiHaTang: %v", err)
	}
}

func TestSessionHookChiChuyenTiepHeaderDaKhai(t *testing.T) {
	h := moiHookGia(t, theoCookie)
	s := hookThu(t, h, func(c *CauHinhSessionHook) { c.Forward = []string{"Cookie", "X-Mot"} })
	r := yeuCauCookie("sid=ok")
	r.Header.Set("Authorization", "Bearer bi-mat")
	r.Header.Set("X-Mot", "1")
	r.Header.Set("X-Khac", "2")
	_, _ = s.NhanDien(context.Background(), r)
	got := h.goi[0]
	if got.Get("Cookie") != "sid=ok" || got.Get("X-Mot") != "1" || got.Get("Authorization") != "" || got.Get("X-Khac") != "" {
		t.Fatalf("header tới hook: %v", got)
	}
	// Nhiều dòng Cookie được nối bằng "; ".
	r2, _ := http.NewRequest("GET", "http://app/x", nil)
	r2.Header.Add("Cookie", "a=1")
	r2.Header.Add("Cookie", "b=2")
	_, _ = s.NhanDien(context.Background(), r2)
	if h.goi[len(h.goi)-1].Get("Cookie") != "a=1; b=2" {
		t.Fatalf("nối cookie: %v", h.goi[len(h.goi)-1])
	}
}

func TestMoiSessionHookTuChoiCauHinhSai(t *testing.T) {
	cacTruongHop := map[string]CauHinhSessionHook{
		"url rỗng":                   {URL: ""},
		"scheme ftp":                 {URL: "ftp://localhost/x"},
		"có user:password":           {URL: "http://u:p@localhost/x"},
		"host ngoài loopback":        {URL: "http://app.vn/api/me"},
		"host hậu tố giả loopback":   {URL: "http://localhost.evil.test/x"},
		"allow_hosts ký tự đại diện": {URL: "http://localhost/x", AllowHosts: []string{"*.vn"}},
		"allow_hosts rỗng":           {URL: "http://localhost/x", AllowHosts: []string{""}},
		"subject_path __proto__":     {URL: "http://localhost/x", SubjectPath: "user.__proto__"},
		"subject_path 6 đoạn":        {URL: "http://localhost/x", SubjectPath: "a.b.c.d.e.f"},
		"subject_path ký tự lạ":      {URL: "http://localhost/x", SubjectPath: "user-id"},
		"forward header sai":         {URL: "http://localhost/x", Forward: []string{"Co Okie"}},
		"forward quá 8":              {URL: "http://localhost/x", Forward: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i"}},
		"timeout quá 10 s":           {URL: "http://localhost/x", Timeout: 11 * time.Second},
		"cache_ttl quá 10 phút":      {URL: "http://localhost/x", TTL: 11 * time.Minute},
	}
	for ten, cfg := range cacTruongHop {
		if _, err := MoiSessionHook(cfg); err == nil {
			t.Errorf("%s: phải bị từ chối", ten)
		}
	}
	// Hợp lệ: loopback, [::1], host trong allow_hosts (khớp nguyên tên, có hoặc không port).
	for _, cfg := range []CauHinhSessionHook{
		{URL: "http://localhost:3000/api/me"}, {URL: "http://127.0.0.1:3000/api/me"}, {URL: "http://[::1]:3000/api/me"},
		{URL: "http://app:3000/api/me", AllowHosts: []string{"app:3000"}}, {URL: "http://app:3000/api/me", AllowHosts: []string{"app"}},
	} {
		if _, err := MoiSessionHook(cfg); err != nil {
			t.Errorf("%s phải hợp lệ: %v", cfg.URL, err)
		}
	}
}
