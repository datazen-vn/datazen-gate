package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

const idPhienCu = "7c1d9a52-3b6e-4f08-8a2d-9e4b5c6d7e81"

func phienTra(w http.ResponseWriter, tt int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(tt)
	_ = json.NewEncoder(w).Encode(body)
}

func TestTaoPhienThanhCong(t *testing.T) {
	dh := &dongHo{t: goc}
	het := goc.Add(time.Hour)
	h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) {
		phienTra(w, 201, map[string]any{
			"id": "cs_1", "url": "https://pay.test/c/abc", "expires_at": het.Format(time.RFC3339),
			"replace_after": het.Add(-10 * time.Minute).Format(time.RFC3339), "future": 1,
		})
	})
	k := khachThu(h, dh, nil)
	p, err := k.TaoPhien(context.Background(), TaoPhienYeuCau{MaKhach: "cus_1", MaGoi: "pro", SuccessURL: "https://app.vn/billing/return", CancelURL: "https://app.vn/pricing"})
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "cs_1" || p.URL != "https://pay.test/c/abc" || !p.HetHan.Equal(het) || p.DoiSau == nil || !p.DoiSau.Equal(het.Add(-10*time.Minute)) {
		t.Fatalf("phiên: %+v", p)
	}
	g := h.cuoi()
	if g.phuongThuc != "POST" || g.duongDanTho != "/v1/checkout-sessions" || g.header.Get("Content-Type") != "application/json" {
		t.Fatalf("lời gọi: %+v", g)
	}
	var than map[string]string
	_ = json.Unmarshal([]byte(g.than), &than)
	if than["customer_ref"] != "cus_1" || than["plan_code"] != "pro" || than["success_url"] != "https://app.vn/billing/return" || than["cancel_url"] != "https://app.vn/pricing" {
		t.Fatalf("thân: %v", than)
	}
	if _, co := than["mode"]; co {
		t.Fatal("môi trường do tiền tố khoá quyết định, không gửi mode")
	}
}

func TestTaoPhienHubCuKhongCoReplaceAfter(t *testing.T) {
	dh := &dongHo{t: goc}
	h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) {
		phienTra(w, 200, map[string]any{"id": "cs_1", "url": "https://pay.test/c/abc", "expires_at": goc.Add(time.Hour).Format(time.RFC3339)})
	})
	p, err := khachThu(h, dh, nil).TaoPhien(context.Background(), TaoPhienYeuCau{MaKhach: "cus_1", MaGoi: "pro"})
	if err != nil || p.DoiSau != nil {
		t.Fatalf("hub cũ: %+v %v", p, err)
	}
}

func TestTaoPhienDauVaoSaiKhongChamHub(t *testing.T) {
	dh := &dongHo{t: goc}
	h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) { phienTra(w, 201, nil) })
	k := khachThu(h, dh, nil) // khoá dz_test_
	cacTruongHop := []struct {
		ten string
		y   TaoPhienYeuCau
		ma  string
	}{
		{"customer_ref sai", TaoPhienYeuCau{MaKhach: "..", MaGoi: "pro"}, LoiMaKhach},
		{"plan_code hoa", TaoPhienYeuCau{MaKhach: "c", MaGoi: "Pro"}, LoiCauHinh},
		{"plan_code rỗng", TaoPhienYeuCau{MaKhach: "c", MaGoi: ""}, LoiCauHinh},
		{"plan_code kết thúc bằng gạch", TaoPhienYeuCau{MaKhach: "c", MaGoi: "pro-"}, LoiCauHinh},
		{"success_url http không phải loopback", TaoPhienYeuCau{MaKhach: "c", MaGoi: "pro", SuccessURL: "http://app.vn/x"}, LoiCauHinh},
		{"success_url loopback không cổng", TaoPhienYeuCau{MaKhach: "c", MaGoi: "pro", SuccessURL: "http://localhost/x"}, LoiCauHinh},
		{"success_url có user:password", TaoPhienYeuCau{MaKhach: "c", MaGoi: "pro", SuccessURL: "https://u:p@app.vn/x"}, LoiCauHinh},
		{"cancel_url quá dài", TaoPhienYeuCau{MaKhach: "c", MaGoi: "pro", CancelURL: "https://app.vn/" + strings.Repeat("a", 1000)}, LoiCauHinh},
	}
	for _, c := range cacTruongHop {
		_, err := k.TaoPhien(context.Background(), c.y)
		if l, ok := ALoi(err); !ok || l.Ma != c.ma {
			t.Errorf("%s: mong %s, có %v", c.ten, c.ma, err)
		}
	}
	if h.soGoi() != 0 {
		t.Fatal("đầu vào sai không được chạm hub")
	}
}

func TestTaoPhienLoopbackChiChoKhoaTest(t *testing.T) {
	dh := &dongHo{t: goc}
	h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) {
		phienTra(w, 201, map[string]any{"id": "cs_1", "url": "https://pay.test/c/abc", "expires_at": goc.Add(time.Hour).Format(time.RFC3339)})
	})
	y := TaoPhienYeuCau{MaKhach: "c", MaGoi: "pro", SuccessURL: "http://localhost:3000/ok"}
	if _, err := khachThu(h, dh, nil).TaoPhien(context.Background(), y); err != nil {
		t.Fatalf("khoá test nhận loopback: %v", err)
	}
	live := khachThu(h, dh, func(cf *CauHinh) { cf.APIKey = "dz_live_khoa" })
	if _, err := live.TaoPhien(context.Background(), y); err == nil {
		t.Fatal("khoá live chỉ nhận https")
	}
}

func TestTaoPhienPhanHoiSaiHinh(t *testing.T) {
	dh := &dongHo{t: goc}
	het := goc.Add(time.Hour).Format(time.RFC3339)
	cacThan := map[string]map[string]any{
		"thiếu id":               {"url": "https://pay.test/c", "expires_at": het},
		"url http không an toàn": {"id": "1", "url": "http://evil.test/c", "expires_at": het},
		"thiếu url":              {"id": "1", "expires_at": het},
		"expires_at hỏng":        {"id": "1", "url": "https://pay.test/c", "expires_at": "mai"},
		"replace_after hỏng":     {"id": "1", "url": "https://pay.test/c", "expires_at": het, "replace_after": "mai"},
	}
	for ten, than := range cacThan {
		h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) { phienTra(w, 201, than) })
		_, err := khachThu(h, dh, nil).TaoPhien(context.Background(), TaoPhienYeuCau{MaKhach: "c", MaGoi: "pro"})
		if l, ok := ALoi(err); !ok || l.Ma != LoiPhanHoi {
			t.Errorf("%s: mong dz_bad_response, có %v", ten, err)
		}
	}
}

func TestTaoPhienLoi4xxThanhCheckoutFailed(t *testing.T) {
	dh := &dongHo{t: goc}
	cacTruongHop := []struct {
		tt      int
		hubCode string
		ma      string
		haTang  bool
	}{
		{404, "plan-khong-ton-tai", LoiCheckout, false},
		{422, "origin-khong-cho-phep", LoiCheckout, false},
		{409, "thue-bao-dang-hieu-luc", LoiCheckout, false},
		{401, "khoa-khong-hop-le", LoiKhoa, false},
		{429, "qua-nhieu-yeu-cau", LoiHanMuc, false},
		{503, "tam-thoi-khong-xu-ly-duoc", LoiMayChu, true},
	}
	for _, c := range cacTruongHop {
		h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) { traLoi(w, c.tt, c.hubCode) })
		_, err := khachThu(h, dh, nil).TaoPhien(context.Background(), TaoPhienYeuCau{MaKhach: "c", MaGoi: "pro"})
		l, ok := ALoi(err)
		if !ok || l.Ma != c.ma || l.HubCode != c.hubCode || l.HTTP != c.tt || l.HaTang() != c.haTang {
			t.Errorf("%d: mong %s/%s, có %+v", c.tt, c.ma, c.hubCode, l)
		}
	}
}

func TestTaoPhien409PhienDaNhanTienMangIDPhienCu(t *testing.T) {
	dh := &dongHo{t: goc}
	cacTruongHop := map[string]string{idPhienCu: idPhienCu, "../x": "", "cs_abc": ""}
	for gui, mong := range cacTruongHop {
		h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) {
			phienTra(w, 409, map[string]any{"code": "phien-da-nhan-tien", "message": "x", "trace_id": "trace-abc12345", "details": map[string]any{"checkout_session_id": gui}})
		})
		_, err := khachThu(h, dh, nil).TaoPhien(context.Background(), TaoPhienYeuCau{MaKhach: "c", MaGoi: "pro"})
		l, ok := ALoi(err)
		if !ok || l.HubCode != "phien-da-nhan-tien" || l.PhienLienQuan != mong {
			t.Errorf("details %q: mong PhienLienQuan %q, có %+v", gui, mong, l)
		}
	}
}

func TestDocPhien(t *testing.T) {
	dh := &dongHo{t: goc}
	het := goc.Add(5 * time.Minute).Format(time.RFC3339)
	h := moiHubGia(t, func(_ int, r *http.Request, _ string, w http.ResponseWriter) {
		if strings.HasSuffix(r.URL.Path, idPhienCu) {
			phienTra(w, 200, map[string]any{"id": idPhienCu, "status": "open", "url": "https://pay.test/c/old", "expires_at": het})
			return
		}
		phienTra(w, 200, map[string]any{"id": "x", "status": "paid", "expires_at": het})
	})
	k := khachThu(h, dh, nil)
	x, err := k.DocPhien(context.Background(), idPhienCu)
	if err != nil || x.TrangThai != "open" || x.URL != "https://pay.test/c/old" {
		t.Fatalf("phiên open: %+v %v", x, err)
	}
	if g := h.cuoi(); g.phuongThuc != "GET" || g.duongDanTho != "/v1/checkout-sessions/"+idPhienCu {
		t.Fatalf("lời gọi: %+v", g)
	}
	x, err = k.DocPhien(context.Background(), "00000000-0000-4000-8000-000000000000")
	if err != nil || x.TrangThai != "paid" || x.URL != "" {
		t.Fatalf("phiên paid không có url: %+v %v", x, err)
	}
}

func TestDocPhienTuChoiIDKhongPhaiUUID(t *testing.T) {
	dh := &dongHo{t: goc}
	h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) { phienTra(w, 200, nil) })
	k := khachThu(h, dh, nil)
	for _, id := range []string{"../x", idPhienCu + "/expire", idPhienCu + "?a=1", "cs_abc", ""} {
		if _, err := k.DocPhien(context.Background(), id); err == nil {
			t.Errorf("id %q phải bị từ chối", id)
		}
	}
	if h.soGoi() != 0 {
		t.Fatal("id sai không được chạm hub")
	}
}

func TestDocPhienSaiHinhVaKhongTim(t *testing.T) {
	dh := &dongHo{t: goc}
	het := goc.Add(time.Minute).Format(time.RFC3339)
	for ten, than := range map[string]map[string]any{
		"thiếu status":      {"id": "1", "expires_at": het},
		"url không an toàn": {"id": "1", "status": "open", "url": "http://evil.test/x", "expires_at": het},
	} {
		h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) { phienTra(w, 200, than) })
		if _, err := khachThu(h, dh, nil).DocPhien(context.Background(), idPhienCu); err == nil {
			t.Errorf("%s: phải lỗi", ten)
		}
	}
	h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) { traLoi(w, 404, "phien-khong-ton-tai") })
	_, err := khachThu(h, dh, nil).DocPhien(context.Background(), idPhienCu)
	if l, ok := ALoi(err); !ok || l.Ma != LoiCheckout || l.HubCode != "phien-khong-ton-tai" {
		t.Fatalf("404: %v", err)
	}
}

func TestPhanTichURLAnToan(t *testing.T) {
	cacTruongHop := []struct {
		url string
		ok  bool
	}{
		{"https://api.datazen.vn", true},
		{"https://api.datazen.vn:8443/x", true},
		{"http://localhost:8080", true},
		{"http://127.0.0.1:8080/", true},
		{"http://localhost", false}, // loopback trơn bị từ chối
		{"http://[::1]:8080", false},
		{"http://api.datazen.vn", false},
		{"https://u:p@api.datazen.vn", false},
		{"ftp://x", false},
		{"", false},
		{"khong-phai-url", false},
	}
	for _, c := range cacTruongHop {
		if _, err := PhanTichURLAnToan(c.url, "x"); (err == nil) != c.ok {
			t.Errorf("%q: mong ok=%v, có %v", c.url, c.ok, err)
		}
	}
}
