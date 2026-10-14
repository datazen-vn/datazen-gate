package danhtinh

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// vector sinh bằng `jose` của sdk (xem testdata/sinh-vector.mjs): chứng minh phần xác minh viết bằng Go khớp một cài đặt độc lập.
type vector struct {
	Now      time.Time         `json:"now"`
	Issuer   string            `json:"issuer"`
	Audience string            `json:"audience"`
	HSSecret string            `json:"hs_secret"`
	Tokens   map[string]string `json:"tokens"`
}

func docVector(t *testing.T) vector {
	t.Helper()
	b, err := os.ReadFile("../../testdata/jwt-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v vector
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func docJWKSFile(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("../../testdata/jwks.json")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// maychuJWKS phục vụ jwks.json và đếm số lần tải.
func maychuJWKS(t *testing.T) (*httptest.Server, *atomic.Int32) {
	var n atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(docJWKSFile(t))
	}))
	t.Cleanup(s.Close)
	return s, &n
}

func yeuCauVoiToken(token string) *http.Request {
	r, _ := http.NewRequest("GET", "http://app.test/x", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

func jwtJWKS(t *testing.T, v vector, url string, sua func(*CauHinhJWT)) *JWT {
	cfg := CauHinhJWT{JWKSURL: url, Issuer: v.Issuer, Audience: v.Audience, Now: func() time.Time { return v.Now }, JWKSCooldown: time.Nanosecond}
	if sua != nil {
		sua(&cfg)
	}
	j, err := MoiJWT(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return j
}

func TestJWTVoiJWKSTheoVectorJose(t *testing.T) {
	v := docVector(t)
	s, _ := maychuJWKS(t)
	j := jwtJWKS(t, v, s.URL, func(c *CauHinhJWT) { c.ThuatToan = thuatToanCongKhai })
	cacTruongHop := []struct {
		ten     string
		mongMa  string
		mongLoi bool // ErrTokenSai
	}{
		{"rs256", "cus_rs", false},
		{"ps256", "cus_ps", false},
		{"es256", "cus_es", false},
		{"es384", "cus_es384", false},
		{"eddsa", "cus_ed", false},
		{"rs256_sub_so", "12345", false},
		{"khong_kid", "cus_1", false}, // có đúng một khoá ES256 ⇒ chọn được dù không kid
		{"rs256_sub_so_qua_lon", "", true},
		{"rs256_sub_bool", "", true},
		{"rs256_thieu_sub", "", true},
		{"het_han", "", true},
		{"thieu_exp", "", true},
		{"sai_aud", "", true},
		{"thieu_aud", "", true},
		{"sai_iss", "", true},
		{"sai_khoa", "", true},
		{"kid_la", "", true},
		{"alg_none", "", true},
		{"hs256_bang_khoa_cong_khai", "", true}, // tấn công nhầm thuật toán
		{"hs256", "", true},                     // HS không nằm trong danh sách của khoá công khai
		{"rac", "", true},
	}
	for _, c := range cacTruongHop {
		ma, err := j.NhanDien(context.Background(), yeuCauVoiToken(v.Tokens[c.ten]))
		if c.mongLoi != errors.Is(err, ErrTokenSai) || (err != nil && !c.mongLoi) {
			t.Errorf("%s: lỗi = %v, mong ErrTokenSai=%v", c.ten, err, c.mongLoi)
			continue
		}
		if ma != c.mongMa {
			t.Errorf("%s: ma = %q, mong %q", c.ten, ma, c.mongMa)
		}
	}
}

func TestJWTKhongToken(t *testing.T) {
	v := docVector(t)
	s, soTai := maychuJWKS(t)
	j := jwtJWKS(t, v, s.URL, nil)
	for _, r := range []*http.Request{yeuCauVoiToken(""), func() *http.Request {
		r := yeuCauVoiToken("")
		r.Header.Set("Authorization", "Basic abc")
		return r
	}()} {
		if ma, err := j.NhanDien(context.Background(), r); ma != "" || err != nil {
			t.Errorf("không có Bearer ⇒ (\"\", nil), có (%q, %v)", ma, err)
		}
	}
	if soTai.Load() != 0 {
		t.Fatal("không có token thì không tải JWKS")
	}
}

func TestJWTDungSaiDongHo(t *testing.T) {
	v := docVector(t)
	s, _ := maychuJWKS(t)
	// het_han_trong_dung_sai hết hạn 3 s trước: qua với dung sai 5 s (mặc định), rớt khi dung sai 1 s.
	j := jwtJWKS(t, v, s.URL, nil)
	if ma, err := j.NhanDien(context.Background(), yeuCauVoiToken(v.Tokens["het_han_trong_dung_sai"])); err != nil || ma != "cus_1" {
		t.Fatalf("dung sai 5 s: %q %v", ma, err)
	}
	j = jwtJWKS(t, v, s.URL, func(c *CauHinhJWT) { c.DungSai = time.Second })
	if _, err := j.NhanDien(context.Background(), yeuCauVoiToken(v.Tokens["het_han_trong_dung_sai"])); !errors.Is(err, ErrTokenSai) {
		t.Fatalf("dung sai 1 s phải từ chối: %v", err)
	}
	// Đồng hồ giả tiến: token hợp lệ hôm nay hết hạn sau đó.
	t2 := v.Now
	j = jwtJWKS(t, v, s.URL, func(c *CauHinhJWT) { c.Now = func() time.Time { return t2 } })
	t2 = time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := j.NhanDien(context.Background(), yeuCauVoiToken(v.Tokens["rs256"])); !errors.Is(err, ErrTokenSai) {
		t.Fatalf("năm 2100 token đã hết hạn: %v", err)
	}
}

func TestJWTClaimChuTheTuyChinh(t *testing.T) {
	v := docVector(t)
	s, _ := maychuJWKS(t)
	j := jwtJWKS(t, v, s.URL, func(c *CauHinhJWT) { c.SubjectClaim = "uid" })
	if ma, err := j.NhanDien(context.Background(), yeuCauVoiToken(v.Tokens["rs256_claim_tuy_chinh"])); err != nil || ma != "cus_uid" {
		t.Fatalf("subject_claim uid: %q %v", ma, err)
	}
}

func TestJWTHMAC(t *testing.T) {
	v := docVector(t)
	j, err := MoiJWT(CauHinhJWT{Secret: []byte(v.HSSecret), Issuer: v.Issuer, Audience: v.Audience, Now: func() time.Time { return v.Now }})
	if err != nil {
		t.Fatal(err)
	}
	cacTruongHop := map[string]struct {
		ma  string
		loi bool
	}{
		"hs256": {"cus_hs", false}, "hs256_het_han": {"", true}, "hs256_sai_khoa": {"", true},
		"hs384":    {"", true}, // mặc định chỉ HS256
		"alg_none": {"", true}, "rs256": {"", true},
	}
	for ten, c := range cacTruongHop {
		ma, err := j.NhanDien(context.Background(), yeuCauVoiToken(v.Tokens[ten]))
		if ma != c.ma || errors.Is(err, ErrTokenSai) != c.loi {
			t.Errorf("%s: (%q, %v), mong (%q, loi=%v)", ten, ma, err, c.ma, c.loi)
		}
	}
	j, _ = MoiJWT(CauHinhJWT{Secret: []byte(v.HSSecret), ThuatToan: []string{"HS384"}, Issuer: v.Issuer, Audience: v.Audience, Now: func() time.Time { return v.Now }})
	if ma, err := j.NhanDien(context.Background(), yeuCauVoiToken(v.Tokens["hs384"])); err != nil || ma != "cus_hs384" {
		t.Fatalf("HS384 khi khai rõ: %q %v", ma, err)
	}
}

func TestJWTLayTokenTuCookieVaHeader(t *testing.T) {
	v := docVector(t)
	cacTruongHop := []struct {
		ten  string
		tu   NguonToken
		sua  func(*http.Request, string)
		mong string
	}{
		{"cookie", NguonToken{Loai: "cookie", Ten: "sid"}, func(r *http.Request, tk string) { r.Header.Set("Cookie", "a=1; sid="+tk+"; b=2") }, "cus_hs"},
		{"cookie có nháy", NguonToken{Loai: "cookie", Ten: "sid"}, func(r *http.Request, tk string) { r.Header.Set("Cookie", `sid="`+tk+`"`) }, "cus_hs"},
		{"cookie tên khác", NguonToken{Loai: "cookie", Ten: "sid"}, func(r *http.Request, tk string) { r.Header.Set("Cookie", "sid2="+tk) }, ""},
		{"header", NguonToken{Loai: "header", Ten: "X-Token"}, func(r *http.Request, tk string) { r.Header.Set("X-Token", tk) }, "cus_hs"},
		{"authorization khi cấu hình cookie ⇒ bỏ qua", NguonToken{Loai: "cookie", Ten: "sid"}, func(r *http.Request, tk string) { r.Header.Set("Authorization", "Bearer "+tk) }, ""},
	}
	for _, c := range cacTruongHop {
		j, err := MoiJWT(CauHinhJWT{Secret: []byte(v.HSSecret), Tu: c.tu, Issuer: v.Issuer, Audience: v.Audience, Now: func() time.Time { return v.Now }})
		if err != nil {
			t.Fatal(err)
		}
		r, _ := http.NewRequest("GET", "http://app.test/", nil)
		c.sua(r, v.Tokens["hs256"])
		if ma, err := j.NhanDien(context.Background(), r); err != nil || ma != c.mong {
			t.Errorf("%s: (%q, %v), mong %q", c.ten, ma, err, c.mong)
		}
	}
}

func TestJWTTokenQuaDai(t *testing.T) {
	v := docVector(t)
	j, _ := MoiJWT(CauHinhJWT{Secret: []byte(v.HSSecret)})
	if _, err := j.NhanDien(context.Background(), yeuCauVoiToken(strings.Repeat("a", 8193))); !errors.Is(err, ErrTokenSai) {
		t.Fatalf("token > 8192 ký tự: %v", err)
	}
}

func TestMoiJWTTuChoiCauHinhSai(t *testing.T) {
	sec := []byte(strings.Repeat("k", 32))
	cacTruongHop := map[string]CauHinhJWT{
		"không có khoá":           {},
		"vừa jwks vừa secret":     {JWKSURL: "https://x", Secret: sec},
		"secret ngắn":             {Secret: []byte("ngan")},
		"alg none":                {Secret: sec, ThuatToan: []string{"none"}},
		"HS với JWKS":             {JWKSURL: "https://x", ThuatToan: []string{"HS256"}},
		"RS với secret":           {Secret: sec, ThuatToan: []string{"RS256"}},
		"subject_claim hỏng":      {Secret: sec, SubjectClaim: "a b"},
		"subject_claim __proto__": {Secret: sec, SubjectClaim: "__proto__"},
		"dung sai quá 60 s":       {Secret: sec, DungSai: 61 * time.Second},
		"dung sai âm":             {Secret: sec, DungSai: -time.Second},
		"from lạ":                 {Secret: sec, Tu: NguonToken{Loai: "query"}},
		"cookie tên hỏng":         {Secret: sec, Tu: NguonToken{Loai: "cookie", Ten: "a=b"}},
		"header tên hoa/ký tự lạ": {Secret: sec, Tu: NguonToken{Loai: "header", Ten: "x token"}},
	}
	for ten, cfg := range cacTruongHop {
		if _, err := MoiJWT(cfg); err == nil {
			t.Errorf("%s: phải bị từ chối", ten)
		}
	}
}
