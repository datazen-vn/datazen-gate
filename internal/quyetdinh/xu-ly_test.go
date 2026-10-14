package quyetdinh

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/datazen-vn/market-gate/internal/hub"
	"github.com/datazen-vn/market-gate/internal/tuyen"
)

func coQuyen(s *thietLap) {
	s.khach = func(int) (int, map[string]any) {
		return 200, snapGia("cus_1", "active", map[string]any{"plan_code": "pro", "features": map[string]bool{"premium": true, "export": true}})
	}
}

func TestChoQuaKhongCanHub(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	for _, uri := range []string{"/api/public/x", "/api/dz/webhook", "/_dz/healthz", "/login", "/billing/return", "/linh-tinh"} {
		kq := s.xuLy(yc{uri: uri, anon: true, header: api})
		if !kq.Quyet.Cho || kq.PhanHoi != nil || kq.Quyet.MaKhach != "" {
			t.Errorf("%s: mong cho qua ẩn danh, có %+v", uri, kq)
		}
	}
	if s.entGoi() != 0 {
		t.Fatal("tuyến allow/bypass không được chạm hub")
	}
}

func TestMacDinhRequireChanTuyenKhongKhai(t *testing.T) {
	s := moiThietLap(t, tuyChon{macDinh: tuyen.Can})
	if kq := s.xuLy(yc{uri: "/linh-tinh", anon: true, header: api}); kq.Quyet.LyDo != LyDoKhongDanhTinh || kq.PhanHoi.Status != 401 {
		t.Fatalf("mặc định require: %+v", kq)
	}
	if kq := s.xuLy(yc{uri: "/api/public/x", anon: true, header: api}); !kq.Quyet.Cho {
		t.Fatal("allow khai rõ vẫn qua")
	}
}

func TestCoQuyenCho(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	coQuyen(s)
	kq := s.xuLy(yc{uri: "/premium/x", header: api})
	if !kq.Quyet.Cho || kq.PhanHoi != nil || kq.Quyet.MaKhach != "cus_1" || kq.Quyet.KetQua == nil || kq.Quyet.KetQua.MaGoi != "pro" {
		t.Fatalf("%+v", kq)
	}
}

func TestChuaDangNhap(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	html := s.xuLy(yc{uri: "/premium/x", anon: true, header: dieuHuong})
	if html.Quyet.LyDo != LyDoKhongDanhTinh || html.Quyet.Status != 401 || html.PhanHoi.Status != 302 || html.PhanHoi.Header.Get("Location") != "/login" {
		t.Fatalf("điều hướng ⇒ 302 login: %+v / %+v", html.Quyet, html.PhanHoi)
	}
	if html.PhanHoi.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("mọi phản hồi gate là no-store")
	}
	a := s.xuLy(yc{uri: "/premium/x", anon: true, header: api})
	if a.PhanHoi.Status != 401 || thanJSON(t, a.PhanHoi)["code"] != "dz_no_identity" {
		t.Fatalf("API ⇒ 401 JSON: %+v %s", a.PhanHoi, a.PhanHoi.Than)
	}
	if s.entGoi() != 0 {
		t.Fatal("chưa đăng nhập không chạm hub")
	}
}

func TestKhongCoQuyenDieuHuongVa402(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	html := s.xuLy(yc{uri: "/premium/x?tab=1&token=bi-mat", header: dieuHuong})
	if html.Quyet.LyDo != LyDoKhongCoQuyen || html.PhanHoi.Status != 302 || html.PhanHoi.Header.Get("Location") != "https://pay.test/c/abc" {
		t.Fatalf("%+v %+v", html.Quyet, html.PhanHoi)
	}
	// success_url ỔN ĐỊNH: không kèm đường dẫn/query của request bị chặn (query có thể mang token/email).
	than := s.thanPOSTCuoi()
	if than["success_url"] != "https://app.test/billing/return" || than["cancel_url"] != "https://app.test/pricing" || than["customer_ref"] != "cus_1" || than["plan_code"] != "pro" {
		t.Fatalf("thân POST: %v", than)
	}
	a := s.xuLy(yc{uri: "/premium/x", header: api})
	b := thanJSON(t, a.PhanHoi)
	if a.PhanHoi.Status != 402 || b["code"] != "dz_not_entitled" || b["status"] != "none" || b["plan_code"] != "pro" || b["checkout_url"] != "https://pay.test/c/abc" {
		t.Fatalf("API ⇒ 402: %s", a.PhanHoi.Than)
	}
	if v, co := b["grace_until"]; !co || v != nil {
		t.Fatalf("grace_until phải có và null: %s", a.PhanHoi.Than)
	}
	if s.soPOST() != 1 {
		t.Fatalf("phiên được đệm theo (khách, gói): %d POST", s.soPOST())
	}
}

func TestNonGETKhongBaoGio302(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	for _, pt := range []string{"POST", "PUT", "DELETE", "PATCH"} {
		kq := s.xuLy(yc{phuongThuc: pt, uri: "/premium/x", header: dieuHuong})
		if kq.PhanHoi.Status != 402 {
			t.Errorf("%s: mong 402, có %d", pt, kq.PhanHoi.Status)
		}
	}
	head := s.xuLy(yc{phuongThuc: "HEAD", uri: "/premium/x", header: api})
	if head.PhanHoi.Status != 402 || len(head.PhanHoi.Than) != 0 {
		t.Fatalf("HEAD ⇒ 402 không thân: %+v", head.PhanHoi)
	}
}

func TestTaoPhienLoiVan402KhongCoCheckoutURL(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	s.phien = func(int) (int, map[string]any) { return 500, map[string]any{"code": "loi"} }
	kq := s.xuLy(yc{uri: "/premium/x", header: dieuHuong})
	if kq.PhanHoi.Status != 402 {
		t.Fatalf("mong 402 (không 503, không 302): %+v", kq.PhanHoi)
	}
	if _, co := thanJSON(t, kq.PhanHoi)["checkout_url"]; co {
		t.Fatalf("checkout_url phải VẮNG, không phải null: %s", kq.PhanHoi.Than)
	}
	if s.soLoi() != 1 {
		t.Fatalf("lỗi tạo phiên đi vào BaoLoi: %d", s.soLoi())
	}
	// Nhớ lỗi 5 s: yêu cầu kế không đập hub nữa.
	s.xuLy(yc{uri: "/premium/x", header: api})
	if s.soPOST() != 1 {
		t.Fatalf("lỗi được nhớ 5 s: %d POST", s.soPOST())
	}
	s.dh.tien(6 * time.Second)
	s.xuLy(yc{uri: "/premium/x", header: api})
	if s.soPOST() != 2 {
		t.Fatalf("sau 5 s thử lại: %d POST", s.soPOST())
	}
}

func TestHubLoiFailClosedVaTuyenOpen(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	s.khach = func(int) (int, map[string]any) { return 503, map[string]any{"code": "x"} }
	kq := s.xuLy(yc{uri: "/premium/x", header: api})
	if kq.Quyet.LyDo != LyDoKhongSan || kq.Quyet.Tu != TuHub || kq.PhanHoi.Status != 503 || thanJSON(t, kq.PhanHoi)["code"] != "dz_hub_unavailable" {
		t.Fatalf("fail-closed: %+v %s", kq.Quyet, kq.PhanHoi.Than)
	}
	// /api/premium/* có fail_mode: open ⇒ cho qua, đánh dấu fail-open.
	op := s.xuLy(yc{uri: "/api/premium/x", header: api})
	if !op.Quyet.Cho || op.Quyet.KetQua == nil || op.Quyet.KetQua.Nguon != hub.NguonMoCua {
		t.Fatalf("tuyến open: %+v", op)
	}
	// 4xx của hub KHÔNG được fail-open cứu dù tuyến open.
	s2 := moiThietLap(t, tuyChon{})
	s2.khach = func(int) (int, map[string]any) { return 403, map[string]any{"code": "x"} }
	if kq := s2.xuLy(yc{uri: "/api/premium/x", header: api}); kq.Quyet.Cho || kq.PhanHoi.Status != 503 {
		t.Fatalf("403 không được cứu: %+v", kq)
	}
}

func TestTrangThaiLa503(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	s.khach = func(int) (int, map[string]any) {
		return 200, snapGia("cus_1", "paused", map[string]any{"features": map[string]bool{"premium": true}})
	}
	kq := s.xuLy(yc{uri: "/premium/x", header: api})
	if kq.Quyet.Cho || kq.Quyet.LyDo != LyDoKhongSan || kq.PhanHoi.Status != 503 {
		t.Fatalf("status lạ ⇒ 503, không 402, không cho qua: %+v", kq)
	}
}

func TestVongLap508(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	kq := s.xuLy(yc{uri: "/premium/x", header: map[string]string{CongHopHeader: "1"}})
	if kq.Quyet.LyDo != LyDoVong || kq.PhanHoi.Status != 508 || thanJSON(t, kq.PhanHoi)["code"] != "dz_gate_loop" {
		t.Fatalf("%+v", kq)
	}
	// Cả tuyến bypass cũng bị chặn vòng, và không chạm hub.
	if kq := s.xuLy(yc{uri: "/api/public/x", anon: true, header: map[string]string{CongHopHeader: "1"}}); kq.Quyet.LyDo != LyDoVong {
		t.Fatalf("vòng lặp chặn trước mọi việc: %+v", kq)
	}
	if s.entGoi() != 0 {
		t.Fatal("508 không chạm hub")
	}
}

func TestAnHanVaFeatureThieu(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	anHan := hetHanSau(s.dh, 72*time.Hour) // mốc cố định: hub luôn trả cùng grace_until
	s.khach = func(int) (int, map[string]any) {
		return 200, snapGia("cus_1", "past_due", map[string]any{"features": map[string]bool{"premium": true}, "grace_until": anHan})
	}
	kq := s.xuLy(yc{uri: "/premium/x", header: api})
	if !kq.Quyet.Cho || !kq.Quyet.AnHan {
		t.Fatalf("ân hạn: %+v", kq)
	}
	// /api/premium/* cần feature `export` mà gói không có ⇒ 402.
	if kq := s.xuLy(yc{uri: "/api/premium/x", header: api}); kq.Quyet.LyDo != LyDoKhongCoQuyen {
		t.Fatalf("thiếu feature ⇒ 402: %+v", kq)
	}
	s.dh.tien(73 * time.Hour)
	kq = s.xuLy(yc{uri: "/premium/x", header: api})
	b := thanJSON(t, kq.PhanHoi)
	if kq.Quyet.Cho || b["status"] != "past_due" || b["grace_until"] == nil {
		t.Fatalf("qua ân hạn ⇒ 402 kèm grace_until: %s", kq.PhanHoi.Than)
	}
	if !strings.HasSuffix(b["grace_until"].(string), "Z") {
		t.Fatalf("grace_until phải ở UTC: %v", b["grace_until"])
	}
}

func TestSoftKhongBaoGioChan(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	// chưa đăng nhập: qua ẩn danh
	if kq := s.xuLy(yc{uri: "/pricing", anon: true, header: api}); !kq.Quyet.Cho || kq.Quyet.MaKhach != "" {
		t.Fatalf("soft chưa đăng nhập: %+v", kq)
	}
	// đăng nhập, chưa có quyền: vẫn qua, kèm kết quả quyền
	kq := s.xuLy(yc{uri: "/pricing", header: api})
	if !kq.Quyet.Cho || kq.Quyet.KetQua == nil || kq.Quyet.KetQua.HieuLuc {
		t.Fatalf("soft chưa có quyền: %+v", kq)
	}
	// hub chết: vẫn qua (fail-open), giữ customer_ref
	s.khach = func(int) (int, map[string]any) { return 500, map[string]any{} }
	s.dh.tien(time.Minute)
	kq = s.xuLy(yc{uri: "/pricing", header: api})
	if !kq.Quyet.Cho || kq.Quyet.MaKhach != "cus_1" || kq.Quyet.KetQua == nil || kq.Quyet.KetQua.Nguon != hub.NguonMoCua {
		t.Fatalf("soft hub chết (5xx ⇒ fail-open có kết quả): %+v", kq)
	}
	// Lỗi KHÔNG cứu được bằng fail-open (403) vẫn không làm trang soft lỗi: cho qua, upstream thấy source fail-open.
	s.khach = func(int) (int, map[string]any) { return 403, map[string]any{} }
	s.dh.tien(time.Minute)
	kq = s.xuLy(yc{uri: "/pricing", header: api})
	if !kq.Quyet.Cho || kq.Quyet.MaKhach != "cus_1" || kq.Quyet.KetQua != nil || kq.Nguon != hub.NguonMoCua {
		t.Fatalf("soft hub 403: %+v", kq)
	}
}

func TestDanhTinhKhongBietLa503HoacFailOpen(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	kq := s.xuLy(yc{uri: "/premium/x", header: map[string]string{"X-User-Hong": "1", "Accept": "application/json"}, anon: true})
	if kq.Quyet.LyDo != LyDoKhongSan || kq.Quyet.Tu != TuDanhTinh || kq.PhanHoi.Status != 503 || thanJSON(t, kq.PhanHoi)["code"] != "dz_identity_unavailable" {
		t.Fatalf("%+v %s", kq.Quyet, kq.PhanHoi.Than)
	}
	op := s.xuLy(yc{uri: "/api/premium/x", header: map[string]string{"X-User-Hong": "1", "Accept": "application/json"}, anon: true})
	if !op.Quyet.Cho || op.Nguon != hub.NguonMoCua || op.Quyet.MaKhach != "" {
		t.Fatalf("tuyến open + danh tính không xác định ⇒ cho qua ẩn danh, đánh dấu fail-open: %+v", op)
	}
}

func TestCustomerRefSaiHinhLaChuaDangNhap(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	for _, ref := range []string{"..", "a b", strings.Repeat("a", 201)} {
		kq := s.xuLy(yc{uri: "/premium/x", header: map[string]string{"X-User": ref, "Accept": "application/json"}})
		if kq.Quyet.LyDo != LyDoKhongDanhTinh {
			t.Errorf("ref %q ⇒ chưa đăng nhập: %+v", ref, kq.Quyet)
		}
	}
	if s.entGoi() != 0 {
		t.Fatal("ref sai không chạm hub")
	}
}

func TestChiTuyenCoMaGoiRieng(t *testing.T) {
	s := moiThietLap(t, tuyChon{routes: []tuyen.Dong{{Khop: "/team/*", HanhDong: tuyen.Can, MaGoi: "team"}}})
	kq := s.xuLy(yc{uri: "/team/x", header: api})
	if thanJSON(t, kq.PhanHoi)["plan_code"] != "team" || s.thanPOSTCuoi()["plan_code"] != "team" {
		t.Fatalf("plan_code riêng của tuyến: %s %v", kq.PhanHoi.Than, s.thanPOSTCuoi())
	}
}

func TestHostKhongThuocOriginsRoiVeOriginDau(t *testing.T) {
	s := moiThietLap(t, tuyChon{paywall: &CauHinhPaywall{MaGoi: "pro", PublicOrigins: []string{"https://app.test", "https://www.app.test"}, SuccessPath: "/ok"}})
	for host, mong := range map[string]string{"www.app.test": "https://www.app.test/ok", "WWW.APP.TEST": "https://www.app.test/ok", "ke-tan-cong.test": "https://app.test/ok"} {
		r, _ := http.NewRequest("GET", "http://"+host+"/premium/x", nil)
		r.RequestURI, r.Host = "/premium/x", host
		r.Header.Set("X-User", "cus_1")
		r.Header.Set("Accept", "application/json")
		s.cong.XuLy(r.Context(), r)
		if got := s.thanPOSTCuoi()["success_url"]; got != mong {
			t.Errorf("host %s: success_url %q, mong %q", host, got, mong)
		}
		s.cong.Paywall.Quen("cus_1")
	}
}
