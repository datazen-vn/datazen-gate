package cauhinh

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var gioGoc = time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)

func TestDungVaChayMotVongThat(t *testing.T) {
	var soEnt, soPhien atomic.Int32
	hubGia := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/entitlements") {
			soEnt.Add(1)
			ma := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/customers/"), "/entitlements")
			if ma == "cus_tra" {
				_ = json.NewEncoder(w).Encode(map[string]any{"customer_ref": ma, "status": "active", "plan_code": "pro", "features": map[string]bool{"dashboard": true}})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"customer_ref": ma, "status": "none", "features": map[string]bool{}})
			return
		}
		soPhien.Add(1)
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "1", "url": "https://pay.test/c/abc", "expires_at": gioGoc.Add(time.Hour).Format(time.RFC3339), "replace_after": gioGoc.Add(50 * time.Minute).Format(time.RFC3339)})
	}))
	defer hubGia.Close()

	cfg, err := Doc(strings.NewReader(`{
	  "gate": {"listen": ":8080", "upstream": "http://app:3000"},
	  "hub": {"base_url": "` + hubGia.URL + `", "cache_ttl": "60s", "fail_mode": "closed"},
	  "identity": [{"type": "jwt", "secret_env": "DZ_JWT_SECRET", "issuer": "https://app.test", "audience": "app"}],
	  "routes": [{"match": "/dashboard/*", "action": "require", "feature": "dashboard"}],
	  "paywall": {"plan_code": "pro", "public_origins": ["https://app.test"], "login_url": "/login", "success_path": "/billing/return"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	cfg.MacDinh()
	cong, err := cfg.Dung(PhuThuoc{Env: envTest, HTTP: hubGia.Client(), Now: func() time.Time { return gioGoc }, UserAgent: "dz-gate/test"})
	if err != nil {
		t.Fatal(err)
	}

	token := func(sub string) string {
		tk := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": sub, "iss": "https://app.test", "aud": "app", "exp": gioGoc.Add(time.Hour).Unix()})
		s, _ := tk.SignedString([]byte(strings.Repeat("s", 32)))
		return s
	}
	yeuCau := func(uri, tk string) *http.Request {
		r := httptest.NewRequest("GET", uri, nil)
		r.Host = "app.test"
		r.Header.Set("Accept", "application/json")
		if tk != "" {
			r.Header.Set("Authorization", "Bearer "+tk)
		}
		return r
	}

	if kq := cong.XuLy(context.Background(), yeuCau("/dashboard/x", token("cus_tra"))); !kq.Quyet.Cho || kq.Quyet.MaKhach != "cus_tra" {
		t.Fatalf("khách đã trả phải qua: %+v", kq)
	}
	kq := cong.XuLy(context.Background(), yeuCau("/dashboard/x", token("cus_chua")))
	if kq.Quyet.Cho || kq.PhanHoi.Status != 402 || !strings.Contains(string(kq.PhanHoi.Than), "https://pay.test/c/abc") {
		t.Fatalf("khách chưa trả ⇒ 402 kèm checkout_url: %+v %s", kq.Quyet, kq.PhanHoi.Than)
	}
	if kq := cong.XuLy(context.Background(), yeuCau("/dashboard/x", "")); kq.PhanHoi.Status != 401 {
		t.Fatalf("chưa đăng nhập ⇒ 401: %+v", kq.PhanHoi)
	}
	if kq := cong.XuLy(context.Background(), yeuCau("/dashboard/x", "khong.phai.token")); kq.PhanHoi.Status != 401 {
		t.Fatalf("token rác ⇒ 401: %+v", kq.PhanHoi)
	}
	if kq := cong.XuLy(context.Background(), yeuCau("/billing/return", "")); !kq.Quyet.Cho {
		t.Fatal("success_path bypass")
	}
	if soEnt.Load() != 2 || soPhien.Load() != 1 {
		t.Fatalf("hub: %d entitlements, %d phiên", soEnt.Load(), soPhien.Load())
	}
}

func TestDungTuChoiCauHinhSai(t *testing.T) {
	c := docMau(t)
	c.Hub.FailMode = "maybe"
	if _, err := c.Dung(PhuThuoc{Env: envTest}); err == nil || !strings.Contains(err.Error(), "hub.fail_mode") {
		t.Fatalf("%v", err)
	}
	if _, err := docMau(t).Dung(PhuThuoc{}); err == nil {
		t.Fatal("thiếu Env phải lỗi")
	}
}
