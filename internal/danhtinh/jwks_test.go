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

func TestDocJWKSTheoFileJose(t *testing.T) {
	khoa, err := DocJWKS(docJWKSFile(t))
	if err != nil {
		t.Fatal(err)
	}
	loai := map[string]string{}
	for _, k := range khoa {
		loai[k.kid] = k.loai
	}
	mong := map[string]string{"rsa-1": "RSA", "ps-1": "RSA", "ec-1": "EC:P-256", "ec384-1": "EC:P-384", "ed-1": "OKP"}
	for kid, l := range mong {
		if loai[kid] != l {
			t.Errorf("kid %s: loại %q, mong %q", kid, loai[kid], l)
		}
	}
}

func TestDocJWKSBoQuaKhoaKhongDungDuoc(t *testing.T) {
	// RSA 512-bit (quá yếu), use=enc, kty lạ, EC điểm không nằm trên đường cong, Ed25519 sai độ dài: đều bị bỏ, KHÔNG làm hỏng cả tập.
	than := `{"keys":[
	  {"kty":"RSA","kid":"yeu","n":"` + strings.Repeat("A", 86) + `","e":"AQAB"},
	  {"kty":"EC","kid":"enc","crv":"P-256","use":"enc","x":"AA","y":"AA"},
	  {"kty":"oct","kid":"lạ","k":"AAAA"},
	  {"kty":"EC","kid":"lech","crv":"P-256","x":"AQ","y":"AQ"},
	  {"kty":"OKP","kid":"ngan","crv":"Ed25519","x":"AQ"}
	]}`
	if _, err := DocJWKS([]byte(than)); err == nil {
		t.Fatal("không còn khoá dùng được ⇒ phải lỗi")
	}
	for _, hong := range []string{`nope`, `{"keys":"x"}`, `{}`} {
		if _, err := DocJWKS([]byte(hong)); err == nil {
			t.Errorf("%q phải lỗi", hong)
		}
	}
}

func jwksGia(t *testing.T, xuLy http.HandlerFunc) (*JWKS, *atomic.Int32, *dongHoJWKS) {
	var n atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		xuLy(w, r)
	}))
	t.Cleanup(s.Close)
	dh := &dongHoJWKS{t: time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)}
	return MoiJWKS(s.URL, s.Client(), time.Second, 30*time.Second, dh.now), &n, dh
}

type dongHoJWKS struct {
	mu sync.Mutex
	t  time.Time
}

func (d *dongHoJWKS) now() time.Time { d.mu.Lock(); defer d.mu.Unlock(); return d.t }
func (d *dongHoJWKS) tien(x time.Duration) {
	d.mu.Lock()
	d.t = d.t.Add(x)
	d.mu.Unlock()
}

func phucVuFile(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write(docJWKSFile(t)) }
}

func TestJWKSChiTaiMotLanVaTaiLaiKhiQuaCuHoacKidLaChiuCooldown(t *testing.T) {
	j, soTai, dh := jwksGia(t, phucVuFile(t))
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := j.Khoa(ctx, "rsa-1", "RS256"); err != nil {
			t.Fatal(err)
		}
	}
	if soTai.Load() != 1 {
		t.Fatalf("5 lần dùng chỉ tải 1 lần: %d", soTai.Load())
	}
	// kid lạ trong cooldown: KHÔNG tải lại (kẻ tấn công không đập được máy chủ JWKS bằng kid bậy).
	for i := 0; i < 20; i++ {
		if _, err := j.Khoa(ctx, "khong-co", "RS256"); !errors.Is(err, ErrTokenSai) {
			t.Fatalf("kid lạ: %v", err)
		}
	}
	if soTai.Load() != 1 {
		t.Fatalf("kid lạ trong cooldown không được tải lại: %d", soTai.Load())
	}
	dh.tien(31 * time.Second) // hết cooldown ⇒ một lần tải lại cho kid lạ
	_, _ = j.Khoa(ctx, "khong-co", "RS256")
	_, _ = j.Khoa(ctx, "khong-co", "RS256")
	if soTai.Load() != 2 {
		t.Fatalf("hết cooldown: tải lại đúng 1 lần: %d", soTai.Load())
	}
	dh.tien(11 * time.Minute) // dữ liệu quá 10 phút ⇒ tải lại dù kid quen
	if _, err := j.Khoa(ctx, "rsa-1", "RS256"); err != nil || soTai.Load() != 3 {
		t.Fatalf("quá 10 phút phải làm tươi: %v, %d lần", err, soTai.Load())
	}
}

func TestJWKSXoayKhoaThayKidMoi(t *testing.T) {
	var moi atomic.Bool
	jmoi := []byte(`{"keys":[]}`)
	j, _, dh := jwksGia(t, func(w http.ResponseWriter, r *http.Request) {
		if moi.Load() {
			_, _ = w.Write(jmoi)
			return
		}
		_, _ = w.Write(docJWKSFile(t))
	})
	if _, err := j.Khoa(context.Background(), "ec-1", "ES256"); err != nil {
		t.Fatal(err)
	}
	// App xoay khoá: JWKS mới có thêm kid mới (lấy lại từ file, đổi kid).
	b := string(docJWKSFile(t))
	jmoi = []byte(strings.Replace(b, `"kid": "ec-1"`, `"kid": "ec-moi"`, 1))
	moi.Store(true)
	dh.tien(31 * time.Second)
	if _, err := j.Khoa(context.Background(), "ec-moi", "ES256"); err != nil {
		t.Fatalf("kid mới sau xoay khoá phải tìm được: %v", err)
	}
}

func TestJWKSLoiTaiKhongBaoGioChapNhanTokenVaDungKhoaCu(t *testing.T) {
	var hong atomic.Bool
	j, _, dh := jwksGia(t, func(w http.ResponseWriter, r *http.Request) {
		if hong.Load() {
			http.Error(w, "x", 500)
			return
		}
		phucVuFile(t)(w, r)
	})
	// Chưa từng tải + máy chủ JWKS chết ⇒ LoiHaTang (không phải ErrTokenSai).
	hong.Store(true)
	_, err := j.Khoa(context.Background(), "rsa-1", "RS256")
	var ha *LoiHaTang
	if !errors.As(err, &ha) || errors.Is(err, ErrTokenSai) {
		t.Fatalf("mong *LoiHaTang, có %v", err)
	}
	hong.Store(false)
	dh.tien(40 * time.Second) // hết đệm âm (30 s + jitter ≤ 20%)
	if _, err := j.Khoa(context.Background(), "rsa-1", "RS256"); err != nil {
		t.Fatal(err)
	}
	// Đã có khoá: máy chủ chết lúc làm tươi ⇒ vẫn dùng khoá cũ nếu tìm được.
	hong.Store(true)
	dh.tien(11 * time.Minute)
	if _, err := j.Khoa(context.Background(), "rsa-1", "RS256"); err != nil {
		t.Fatalf("dùng khoá cũ khi làm tươi lỗi: %v", err)
	}
	// Nhưng kid lạ khi máy chủ chết ⇒ không biết ⇒ lỗi hạ tầng, không "token sai".
	if _, err := j.Khoa(context.Background(), "khong-co", "RS256"); !errors.As(err, &ha) {
		t.Fatalf("kid lạ + JWKS chết ⇒ LoiHaTang: %v", err)
	}
}

func TestJWKSKhongTheoChuyenHuongVaThanQuaLon(t *testing.T) {
	var theoDen atomic.Int32
	dich := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { theoDen.Add(1) }))
	defer dich.Close()
	j, _, _ := jwksGia(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, dich.URL, http.StatusFound) })
	var ha *LoiHaTang
	if _, err := j.Khoa(context.Background(), "rsa-1", "RS256"); !errors.As(err, &ha) || theoDen.Load() != 0 {
		t.Fatalf("3xx ⇒ LoiHaTang, không theo: %v (theo %d)", err, theoDen.Load())
	}
	j, _, _ = jwksGia(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"keys":[],"pad":"` + strings.Repeat("x", 300<<10) + `"}`))
	})
	if _, err := j.Khoa(context.Background(), "rsa-1", "RS256"); !errors.As(err, &ha) {
		t.Fatalf("thân > 256 KB ⇒ LoiHaTang: %v", err)
	}
}

func TestJWKSKhoaTheoThuatToanVaKid(t *testing.T) {
	j, _, _ := jwksGia(t, phucVuFile(t))
	ctx := context.Background()
	cacTruongHop := []struct {
		kid, alg string
		ok       bool
	}{
		{"rsa-1", "RS256", true},
		{"rsa-1", "PS256", true}, // cùng kiểu RSA
		{"rsa-1", "ES256", false},
		{"ec-1", "ES256", true},
		{"ec-1", "ES384", false}, // đường cong không khớp
		{"ec384-1", "ES384", true},
		{"ed-1", "EdDSA", true},
		{"", "ES256", true}, // đúng một khoá ES256
		{"", "ES384", true},
		{"", "RS256", false}, // hai khoá RSA, không kid ⇒ mơ hồ
		{"", "HS256", false},
		{"ec-1", "none", false},
	}
	for _, c := range cacTruongHop {
		_, err := j.Khoa(ctx, c.kid, c.alg)
		if (err == nil) != c.ok {
			t.Errorf("kid=%q alg=%s: ok=%v, lỗi %v", c.kid, c.alg, c.ok, err)
		}
	}
}

func TestJWKSDongThoiChiTaiMotLan(t *testing.T) {
	thaDi := make(chan struct{})
	j, soTai, _ := jwksGia(t, func(w http.ResponseWriter, r *http.Request) {
		<-thaDi
		phucVuFile(t)(w, r)
	})
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := j.Khoa(context.Background(), "rsa-1", "RS256"); err != nil {
				t.Errorf("%v", err)
			}
		}()
	}
	for soTai.Load() < 1 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	close(thaDi)
	wg.Wait()
	if soTai.Load() != 1 {
		t.Fatalf("20 lời gọi đầu tiên gộp thành 1 lần tải: %d", soTai.Load())
	}
}

func TestJWKSChetThiDemAmKhongBaoTaiMoiRequest(t *testing.T) {
	// T2 (panel an ninh): JWKS hỏng KHÔNG được làm MỌI request tải lại và chờ tới timeout.
	j, soTai, dh := jwksGia(t, func(w http.ResponseWriter, r *http.Request) { http.Error(w, "x", 500) })
	var ha *LoiHaTang
	for i := 0; i < 30; i++ {
		if _, err := j.Khoa(context.Background(), "rsa-1", "RS256"); !errors.As(err, &ha) || errors.Is(err, ErrTokenSai) {
			t.Fatalf("lần %d: phải là LoiHaTang (không phải 'token sai'): %v", i, err)
		}
	}
	if soTai.Load() != 1 {
		t.Fatalf("30 request khi JWKS chết ⇒ đúng 1 lần tải: %d", soTai.Load())
	}
	dh.tien(29 * time.Second) // còn trong đệm âm tối thiểu (cooldown 30 s)
	_, _ = j.Khoa(context.Background(), "rsa-1", "RS256")
	if soTai.Load() != 1 {
		t.Fatalf("29 s: vẫn trong đệm âm: %d", soTai.Load())
	}
	dh.tien(8 * time.Second) // 37 s > 30 s + 20% jitter
	_, _ = j.Khoa(context.Background(), "rsa-1", "RS256")
	if soTai.Load() != 2 {
		t.Fatalf("hết đệm âm ⇒ thử lại đúng một lần: %d", soTai.Load())
	}
}

func TestJWKSDemAmVanDungKhoaCuKhiTimDuoc(t *testing.T) {
	var hong atomic.Bool
	j, soTai, dh := jwksGia(t, func(w http.ResponseWriter, r *http.Request) {
		if hong.Load() {
			http.Error(w, "x", 500)
			return
		}
		phucVuFile(t)(w, r)
	})
	if _, err := j.Khoa(context.Background(), "rsa-1", "RS256"); err != nil {
		t.Fatal(err)
	}
	hong.Store(true)
	dh.tien(11 * time.Minute) // quá cũ ⇒ làm tươi lỗi ⇒ vào đệm âm, nhưng khoá cũ vẫn dùng được
	for i := 0; i < 10; i++ {
		if _, err := j.Khoa(context.Background(), "rsa-1", "RS256"); err != nil {
			t.Fatalf("lần %d: khoá cũ vẫn phải dùng được: %v", i, err)
		}
	}
	if soTai.Load() != 2 {
		t.Fatalf("1 lần tải đầu + 1 lần làm tươi lỗi, rồi đệm âm: %d", soTai.Load())
	}
	// kid lạ trong đệm âm ⇒ vẫn là lỗi hạ tầng (không đổ oan cho token)
	var ha *LoiHaTang
	if _, err := j.Khoa(context.Background(), "khong-co", "RS256"); !errors.As(err, &ha) {
		t.Fatalf("%v", err)
	}
	if soTai.Load() != 2 {
		t.Fatalf("kid lạ trong đệm âm không tải lại: %d", soTai.Load())
	}
}
