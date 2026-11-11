package vanhanh

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/datazen-vn/datazen-gate/internal/hub"
)

func TestBamKhach(t *testing.T) {
	b := MoiBam("muoi-co-dinh")
	h := b.Khach("user@example.com")
	if !regexp.MustCompile(`^[0-9a-f]{16}$`).MatchString(h) {
		t.Fatalf("16 hex: %q", h)
	}
	if h != b.Khach("user@example.com") || h == b.Khach("user2@example.com") {
		t.Fatal("cùng ref ⇒ cùng giá trị; khác ref ⇒ khác")
	}
	if strings.Contains(h, "example") {
		t.Fatal("không lộ ref")
	}
	if MoiBam("khac").Khach("user@example.com") == h {
		t.Fatal("salt khác ⇒ băm khác")
	}
	if a, c := MoiBam("").Khach("x"), MoiBam("").Khach("x"); a == c {
		t.Fatal("salt ngẫu nhiên mỗi tiến trình ⇒ không so được giữa các lần chạy")
	}
	if b.Khach("") != "" {
		t.Fatal("ref rỗng ⇒ rỗng")
	}
}

func TestDoLuongVietDungDinhDang(t *testing.T) {
	d := MoiDoLuong()
	d.Cong("dz_gate_requests_total", "ket_qua", "allowed")
	d.Cong("dz_gate_requests_total", "ket_qua", "allowed")
	d.Cong("dz_gate_requests_total", "ket_qua", "not_entitled", "ly_do", `a"b\c`+"\n")
	d.Cong("dz_gate_upstream_errors_total")
	d.ThoiGian(0.01)
	d.ThoiGian(3)
	var b bytes.Buffer
	d.Viet(&b)
	out := b.String()
	cacTruongHop := []string{
		"# TYPE dz_gate_requests_total counter\n",
		`dz_gate_requests_total{ket_qua="allowed"} 2` + "\n",
		`dz_gate_requests_total{ket_qua="not_entitled",ly_do="a\"b\\c\n"} 1` + "\n",
		"dz_gate_upstream_errors_total 1\n",
		`dz_gate_request_duration_seconds_bucket{le="0.025"} 1` + "\n",
		`dz_gate_request_duration_seconds_bucket{le="5"} 2` + "\n",
		`dz_gate_request_duration_seconds_bucket{le="+Inf"} 2` + "\n",
		"dz_gate_request_duration_seconds_count 2\n",
	}
	for _, c := range cacTruongHop {
		if !strings.Contains(out, c) {
			t.Errorf("thiếu %q trong:\n%s", c, out)
		}
	}
	if strings.Count(out, "# TYPE dz_gate_requests_total") != 1 {
		t.Fatal("mỗi tên chỉ một dòng TYPE")
	}
}

func TestDoLuongChanSoChuoi(t *testing.T) {
	d := MoiDoLuong()
	for i := 0; i < soChuoiToiDa+50; i++ {
		d.Cong("x_total", "k", string(rune('a'+i%26))+strings.Repeat("z", i))
	}
	d.Cong("x_total", "k", "a"+strings.Repeat("z", 0)) // chuỗi đã có vẫn tăng được
	var b bytes.Buffer
	d.Viet(&b)
	if n := strings.Count(b.String(), "x_total{"); n != soChuoiToiDa {
		t.Fatalf("giữ tối đa %d chuỗi, có %d", soChuoiToiDa, n)
	}
	if !strings.Contains(b.String(), "dz_gate_metric_series_dropped_total 50") {
		t.Fatalf("phải đếm số chuỗi bị bỏ: %s", b.String())
	}
}

func TestSanSangNhoKetQuaVaKhongLoThongBaoLoi(t *testing.T) {
	var soGoi atomic.Int32
	now := time.Date(2026, 10, 14, 10, 0, 0, 0, time.UTC)
	ss := MoiSanSang(func() time.Time { return now },
		Kiem{"upstream", func(context.Context) error { return errors.New("dial tcp 10.0.0.5:3000: connection refused") }},
		Kiem{"hub", func(context.Context) error { soGoi.Add(1); return &hub.Loi{Ma: hub.LoiKhoa, ChiTiet: "x"} }},
	)
	ok, kq := ss.Chay(context.Background())
	if ok || kq["upstream"] != "loi" || kq["hub"] != hub.LoiKhoa {
		t.Fatalf("%v %v", ok, kq)
	}
	for _, v := range kq {
		if strings.Contains(v, "10.0.0.5") {
			t.Fatal("không lộ địa chỉ nội bộ")
		}
	}
	ss.Chay(context.Background())
	if soGoi.Load() != 1 {
		t.Fatalf("kết quả nhớ 5 s: %d lần kiểm hub", soGoi.Load())
	}
	now = now.Add(6 * time.Second)
	ss.Chay(context.Background())
	if soGoi.Load() != 2 {
		t.Fatalf("hết 5 s kiểm lại: %d", soGoi.Load())
	}
}

func TestKiemTCP(t *testing.T) {
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	if err := KiemTCP(l.Addr().String())(context.Background()); err != nil {
		t.Fatal(err)
	}
	l.Close()
	if err := KiemTCP(l.Addr().String())(context.Background()); err == nil {
		t.Fatal("cổng đóng phải lỗi")
	}
}

func TestOps(t *testing.T) {
	d := MoiDoLuong()
	d.Cong("dz_gate_requests_total", "ket_qua", "allowed")
	var ready atomic.Bool
	ss := MoiSanSang(nil, Kiem{"hub", func(context.Context) error {
		if ready.Load() {
			return nil
		}
		return errors.New("x")
	}})
	srv := httptest.NewServer(Ops(d, ss))
	defer srv.Close()
	lay := func(pt, p string) (*http.Response, string) {
		req, _ := http.NewRequest(pt, srv.URL+p, nil)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var b bytes.Buffer
		_, _ = b.ReadFrom(res.Body)
		return res, b.String()
	}
	if res, b := lay("GET", "/_dz/healthz"); res.StatusCode != 200 || b != "ok\n" {
		t.Fatalf("healthz: %d %q", res.StatusCode, b)
	}
	if res, b := lay("GET", "/_dz/readyz"); res.StatusCode != 503 || !strings.Contains(b, `"ready":false`) {
		t.Fatalf("readyz chưa sẵn sàng: %d %s", res.StatusCode, b)
	}
	if res, b := lay("GET", "/_dz/metrics"); res.StatusCode != 200 || !strings.Contains(b, `dz_gate_requests_total{ket_qua="allowed"} 1`) || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/plain; version=0.0.4") {
		t.Fatalf("metrics: %d %s", res.StatusCode, b)
	}
	if res, _ := lay("POST", "/_dz/healthz"); res.StatusCode != 405 {
		t.Fatalf("POST: %d", res.StatusCode)
	}
	if res, _ := lay("GET", "/_dz/khac"); res.StatusCode != 404 {
		t.Fatalf("đường lạ: %d", res.StatusCode)
	}
	if res, _ := lay("GET", "/premium/x"); res.StatusCode != 404 {
		t.Fatalf("cổng vận hành không phục vụ đường của khách: %d", res.StatusCode)
	}
	if res, _ := lay("GET", "/_dz/healthz"); res.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("no-store")
	}
}
