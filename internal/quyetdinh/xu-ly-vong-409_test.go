package quyetdinh

import (
	"testing"
	"time"

	"github.com/datazen-vn/datazen-gate/internal/tuyen"
)

// Panel an ninh C1: khách trả tiền gói rẻ không được gọi hub theo tốc độ request của chính họ.

func coQuyenThieuFeature(s *thietLap) {
	s.khach = func(int) (int, map[string]any) {
		return 200, snapGia("cus_1", "active", map[string]any{"plan_code": "pro", "features": map[string]bool{"export": true}})
	}
}

func TestActiveCungGoiThieuFeature402KhongTaoPhienKhongVong(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	coQuyenThieuFeature(s)
	s.phien = func(int) (int, map[string]any) {
		return 409, map[string]any{"code": "thue-bao-dang-hieu-luc", "message": "x", "trace_id": "trace-abc12345"}
	}
	for i := 0; i < 20; i++ {
		kq := s.xuLy(yc{uri: "/premium/x", header: api})
		b := thanJSON(t, kq.PhanHoi)
		if kq.Quyet.Cho || kq.PhanHoi.Status != 402 || b["status"] != "active" || b["plan_code"] != "pro" {
			t.Fatalf("lần %d: %+v %s", i, kq.Quyet, kq.PhanHoi.Than)
		}
		if _, co := b["checkout_url"]; co {
			t.Fatal("không checkout_url")
		}
	}
	if s.entGoi() != 1 || s.soPOST() != 0 || s.soLoi() != 0 {
		t.Fatalf("20 request ⇒ 1 GET, 0 POST, 0 lỗi; có %d GET, %d POST, %d lỗi", s.entGoi(), s.soPOST(), s.soLoi())
	}
	// trình duyệt cũng 402 (không có URL để 302)
	if kq := s.xuLy(yc{uri: "/premium/x", header: dieuHuong}); kq.PhanHoi.Status != 402 {
		t.Fatalf("điều hướng: %d", kq.PhanHoi.Status)
	}
}

func bangGoiKhac() []tuyen.Dong {
	return []tuyen.Dong{{Khop: "/team/*", HanhDong: tuyen.Can, TinhNang: "team-only", MaGoi: "team"}}
}

func TestDoiGoiVanTaoPhien(t *testing.T) {
	s2 := moiThietLap(t, tuyChon{routes: bangGoiKhac()})
	coQuyenThieuFeature(s2)
	kq := s2.xuLy(yc{uri: "/team/x", header: api})
	if kq.Quyet.Cho || s2.soPOST() != 1 || thanJSON(t, kq.PhanHoi)["checkout_url"] == nil {
		t.Fatalf("đổi gói (gói tuyến 'team' ≠ 'pro' đang active) vẫn tạo phiên: POST %d", s2.soPOST())
	}
	if s2.thanPOSTCuoi()["plan_code"] != "team" {
		t.Fatalf("%v", s2.thanPOSTCuoi())
	}
}

func TestHoiLaiSau409ChiKhiDemKhongHieuLucVaChiuTran(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	// hub không nhất quán: luôn none nhưng tạo phiên luôn 409
	s.phien = func(int) (int, map[string]any) { return 409, map[string]any{"code": "thue-bao-dang-hieu-luc"} }
	for i := 0; i < 5; i++ {
		s.xuLy(yc{uri: "/premium/x", header: api})
		s.dh.tien(900 * time.Millisecond)
	}
	if s.entGoi() != 2 || s.soPOST() != 1 {
		t.Fatalf("5 request trong 5 s ⇒ 1 lần hỏi lại + 1 phiên (2 GET, 1 POST); có %d GET, %d POST", s.entGoi(), s.soPOST())
	}
	s.dh.tien(2 * time.Second) // đủ 6 s từ lần đầu
	s.xuLy(yc{uri: "/premium/x", header: api})
	if s.entGoi() != 3 || s.soPOST() != 2 {
		t.Fatalf("hết trần + hết nhớ lỗi ⇒ thử lại một lần: %d GET, %d POST", s.entGoi(), s.soPOST())
	}
}

func TestHoiLaiKhongXoaDemQuyen(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	s.khach = func(n int) (int, map[string]any) {
		if n == 1 {
			return 200, snapGia("cus_1", "none", nil)
		}
		return 200, snapGia("cus_1", "active", map[string]any{"features": map[string]bool{"premium": true}})
	}
	s.phien = func(int) (int, map[string]any) { return 409, map[string]any{"code": "thue-bao-dang-hieu-luc"} }
	for i := 0; i < 3; i++ {
		if kq := s.xuLy(yc{uri: "/premium/x", header: api}); !kq.Quyet.Cho {
			t.Fatalf("lần %d phải qua: %+v", i, kq.Quyet)
		}
	}
	if s.entGoi() != 2 {
		t.Fatalf("sau khi hỏi lại thấy active, các request sau dùng đệm: %d GET", s.entGoi())
	}
}

func TestDemHieuLucMaVan409KhongHoiLai(t *testing.T) {
	// đổi gói + hub 409 bất thường: đệm nói ĐÃ hiệu lực ⇒ không hỏi lại (chỉ hỏi lại khi đệm nói không hiệu lực)
	s := moiThietLap(t, tuyChon{routes: bangGoiKhac()})
	coQuyenThieuFeature(s)
	s.phien = func(int) (int, map[string]any) { return 409, map[string]any{"code": "thue-bao-dang-hieu-luc"} }
	s.xuLy(yc{uri: "/team/x", header: api})
	if s.entGoi() != 1 || s.soPOST() != 1 {
		t.Fatalf("%d GET, %d POST", s.entGoi(), s.soPOST())
	}
}
