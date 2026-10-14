package quyetdinh

import (
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const idPhien = "5b0c2f3e-8d41-4a6b-9c7e-2f1a3b4c5d6e"

// thuBaoSauKhiTra: hub trả none cho tới khi daTra bật.
func thuBaoSauKhiTra(s *thietLap) *atomic.Bool {
	var daTra atomic.Bool
	s.khach = func(int) (int, map[string]any) {
		if daTra.Load() {
			return 200, snapGia("cus_1", "active", map[string]any{"features": map[string]bool{"premium": true}})
		}
		return 200, snapGia("cus_1", "none", nil)
	}
	return &daTra
}

func TestQuayVeDocTuoiBatChapDemKhongQuyen(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	daTra := thuBaoSauKhiTra(s)
	if kq := s.xuLy(yc{uri: "/premium/x", header: dieuHuong}); kq.Quyet.Cho {
		t.Fatal("chưa trả ⇒ chặn")
	}
	daTra.Store(true)
	s.dh.tien(5 * time.Second) // bản none còn tươi (< 15 s)
	if kq := s.xuLy(yc{uri: "/premium/x", header: dieuHuong}); kq.Quyet.Cho || s.entGoi() != 1 {
		t.Fatalf("không tham số ⇒ vẫn theo bản đệm none: %+v (hub %d)", kq.Quyet, s.entGoi())
	}
	back := s.xuLy(yc{uri: "/premium/x?checkout_session_id=" + idPhien, header: dieuHuong})
	if !back.Quyet.Cho || s.entGoi() != 2 {
		t.Fatalf("có checkout_session_id ⇒ đọc tươi, vào ngay: %+v (hub %d)", back.Quyet, s.entGoi())
	}
}

func TestQuayVeThamSoSaiHinhBiBoQua(t *testing.T) {
	for _, q := range []string{"checkout_session_id=cs_abc123", "checkout_session_id=../x", "checkout_session_id=", "checkout_session_id=" + idPhien + "x", "checkout_session_id=" + strings.ToUpper(idPhien)[1:], "x=1"} {
		s := moiThietLap(t, tuyChon{})
		s.xuLy(yc{uri: "/premium/x", header: api})
		s.xuLy(yc{uri: "/premium/x?" + q, header: api})
		if s.entGoi() != 1 {
			t.Errorf("%q: không được đọc tươi (%d lần gọi hub)", q, s.entGoi())
		}
	}
	// uuid IN HOA và giá trị ĐẦU TIÊN được dùng.
	for _, q := range []string{"checkout_session_id=" + strings.ToUpper(idPhien), "checkout_session_id=" + idPhien + "&checkout_session_id=rac"} {
		s := moiThietLap(t, tuyChon{})
		s.xuLy(yc{uri: "/premium/x", header: api})
		s.xuLy(yc{uri: "/premium/x?" + q, header: api})
		if s.entGoi() != 2 {
			t.Errorf("%q: phải đọc tươi (%d lần gọi hub)", q, s.entGoi())
		}
	}
}

func TestQuayVeChiGETHEAD(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	daTra := thuBaoSauKhiTra(s)
	s.xuLy(yc{uri: "/premium/x", header: api})
	daTra.Store(true)
	uri := "/premium/x?checkout_session_id=" + idPhien
	if kq := s.xuLy(yc{phuongThuc: "POST", uri: uri, header: api}); kq.Quyet.Cho || s.entGoi() != 1 {
		t.Fatalf("POST không kích hoạt đọc tươi: %+v", kq.Quyet)
	}
	if kq := s.xuLy(yc{phuongThuc: "HEAD", uri: uri, header: api}); !kq.Quyet.Cho || s.entGoi() != 2 {
		t.Fatalf("HEAD thì có: %+v", kq.Quyet)
	}
}

func TestQuayVeTran1Lan5GiayVaChuaDanhTinhKhongChamHub(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	s.xuLy(yc{uri: "/premium/x", header: api})
	for i := 0; i < 10; i++ {
		s.xuLy(yc{uri: "/premium/x?checkout_session_id=" + idPhien, header: api})
	}
	if s.entGoi() != 2 {
		t.Fatalf("10 lần quay về chỉ tốn 1 lần đọc tươi: %d", s.entGoi())
	}
	s2 := moiThietLap(t, tuyChon{})
	kq := s2.xuLy(yc{uri: "/premium/x?checkout_session_id=" + idPhien, anon: true, header: api})
	if kq.Quyet.LyDo != LyDoKhongDanhTinh || s2.entGoi() != 0 {
		t.Fatalf("chưa nhận diện ⇒ KHÔNG chạm hub: %+v (hub %d)", kq.Quyet, s2.entGoi())
	}
}

func TestQuayVeOSuccessPathBypassLamTuoiKhongBaoGioChan(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	daTra := thuBaoSauKhiTra(s)
	s.xuLy(yc{uri: "/premium/x", header: api})
	daTra.Store(true)
	s.dh.tien(5 * time.Second)
	back := s.xuLy(yc{uri: "/billing/return?checkout_session_id=" + idPhien, header: dieuHuong})
	if !back.Quyet.Cho || back.PhanHoi != nil || back.Quyet.MaKhach != "" || back.Quyet.KetQua != nil {
		t.Fatalf("success_path: cho qua ẩn danh, không tiêm danh tính: %+v", back)
	}
	if s.entGoi() != 2 {
		t.Fatalf("success_path vẫn làm tươi đệm: %d", s.entGoi())
	}
	// Tuyến trả phí ngay sau đó dùng bản vừa tươi: không vòng paywall.
	if kq := s.xuLy(yc{uri: "/premium/x", header: dieuHuong}); !kq.Quyet.Cho || s.entGoi() != 2 {
		t.Fatalf("không vòng paywall sau khi trả: %+v (hub %d)", kq.Quyet, s.entGoi())
	}
}

func TestQuayVeOSuccessPathKhongMangDanhTinhHoacHubLoi(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	// không tham số / sai hình / POST ⇒ không chạm hub
	s.xuLy(yc{uri: "/billing/return", header: api})
	s.xuLy(yc{uri: "/billing/return?checkout_session_id=cs_abc", header: api})
	s.xuLy(yc{phuongThuc: "POST", uri: "/billing/return?checkout_session_id=" + idPhien, header: api})
	// chưa nhận diện ⇒ qua, không chạm hub
	if kq := s.xuLy(yc{uri: "/billing/return?checkout_session_id=" + idPhien, anon: true, header: api}); !kq.Quyet.Cho {
		t.Fatal("success_path không bao giờ chặn")
	}
	if s.entGoi() != 0 {
		t.Fatalf("không được chạm hub: %d", s.entGoi())
	}
	// hub lỗi ⇒ vẫn cho qua (lỗi vào BaoLoi), danh tính lỗi cũng vậy
	s.khach = func(int) (int, map[string]any) { return 503, map[string]any{} }
	if kq := s.xuLy(yc{uri: "/billing/return?checkout_session_id=" + idPhien, header: api}); !kq.Quyet.Cho || kq.PhanHoi != nil {
		t.Fatalf("hub lỗi không chặn success_path: %+v", kq)
	}
	if kq := s.xuLy(yc{uri: "/billing/return?checkout_session_id=" + idPhien, anon: true, header: map[string]string{"X-User-Hong": "1"}}); !kq.Quyet.Cho {
		t.Fatalf("danh tính lỗi không chặn success_path: %+v", kq)
	}
}

func TestQuayVeDocTuoiLoiLa503KhongLuiVeDemNone(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	s.xuLy(yc{uri: "/premium/x", header: api})
	s.khach = func(int) (int, map[string]any) { return 503, map[string]any{"code": "x"} }
	kq := s.xuLy(yc{uri: "/premium/x?checkout_session_id=" + idPhien, header: api})
	if kq.Quyet.LyDo != LyDoKhongSan || kq.PhanHoi.Status != 503 || thanJSON(t, kq.PhanHoi)["code"] != "dz_hub_unavailable" {
		t.Fatalf("đọc tươi lỗi ⇒ 503 trung thực, không 402 từ đệm none: %+v", kq.Quyet)
	}
}

func TestDocTuoiXoaPhienDaDem(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	daTra := thuBaoSauKhiTra(s)
	s.xuLy(yc{uri: "/premium/x", header: api}) // 402 ⇒ POST #1, đệm phiên
	if s.soPOST() != 1 {
		t.Fatal("POST #1")
	}
	daTra.Store(true)
	s.xuLy(yc{uri: "/premium/x?checkout_session_id=" + idPhien, header: api})
	daTra.Store(false)
	s.cong.Quyen.Huy("cus_1") // thuê bao hết hạn rồi
	s.xuLy(yc{uri: "/premium/x", header: api})
	if s.soPOST() != 2 {
		t.Fatalf("hết hạn sau đó xin phiên MỚI, không 302 về phiên đã trả: %d POST", s.soPOST())
	}
}

func TestDocTuoiChuaActiveCungXoaPhienDaDem(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	s.xuLy(yc{uri: "/premium/x", header: api})                                // POST #1
	s.xuLy(yc{uri: "/premium/x?checkout_session_id=" + idPhien, header: api}) // đọc tươi thật, vẫn none ⇒ bỏ phiên ⇒ POST #2
	if s.soPOST() != 2 {
		t.Fatalf("phiên có thể đã bị hub thay ⇒ xin lại: %d POST", s.soPOST())
	}
	s.xuLy(yc{uri: "/premium/x?checkout_session_id=" + idPhien, header: api}) // quá trần ⇒ bản đệm ⇒ không xoá
	if s.soPOST() != 2 {
		t.Fatalf("bản đệm do trần không xoá phiên: %d POST", s.soPOST())
	}
}

func TestXungDot409ThueBaoDangHieuLucHoiLaiMotLan(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	s.khach = func(n int) (int, map[string]any) {
		if n == 1 {
			return 200, snapGia("cus_1", "none", nil)
		}
		return 200, snapGia("cus_1", "active", map[string]any{"features": map[string]bool{"premium": true}})
	}
	s.phien = func(int) (int, map[string]any) {
		return 409, map[string]any{"code": "thue-bao-dang-hieu-luc", "message": "x", "trace_id": "trace-abc12345"}
	}
	kq := s.xuLy(yc{uri: "/premium/x", header: api})
	if !kq.Quyet.Cho || kq.Quyet.MaKhach != "cus_1" || kq.PhanHoi != nil || s.entGoi() != 2 || s.soPOST() != 1 {
		t.Fatalf("hỏi lại thấy active ⇒ cho qua: %+v (hub %d, POST %d)", kq.Quyet, s.entGoi(), s.soPOST())
	}
	// Vẫn không có quyền sau khi hỏi lại ⇒ 402 không checkout_url, đúng MỘT lần hỏi lại, lỗi vào BaoLoi.
	s2 := moiThietLap(t, tuyChon{})
	s2.phien = func(int) (int, map[string]any) { return 409, map[string]any{"code": "thue-bao-dang-hieu-luc"} }
	kq = s2.xuLy(yc{uri: "/premium/x", header: api})
	if kq.Quyet.Cho || kq.PhanHoi.Status != 402 || s2.entGoi() != 2 || s2.soLoi() != 1 {
		t.Fatalf("%+v (hub %d, lỗi %d)", kq.Quyet, s2.entGoi(), s2.soLoi())
	}
	if _, co := thanJSON(t, kq.PhanHoi)["checkout_url"]; co {
		t.Fatal("không checkout_url")
	}
	// Lỗi checkout khác (500) KHÔNG kích hoạt hỏi lại.
	s3 := moiThietLap(t, tuyChon{})
	s3.phien = func(int) (int, map[string]any) { return 500, map[string]any{} }
	s3.xuLy(yc{uri: "/premium/x", header: api})
	if s3.entGoi() != 1 {
		t.Fatalf("500 không hỏi lại: %d", s3.entGoi())
	}
}

func TestPhienDaNhanTienDuaKhachVePhienCu(t *testing.T) {
	const idCu = "7c1d9a52-3b6e-4f08-8a2d-9e4b5c6d7e81"
	conflict := func(int) (int, map[string]any) {
		return 409, map[string]any{"code": "phien-da-nhan-tien", "message": "x", "trace_id": "trace-abc12345", "details": map[string]any{"checkout_session_id": idCu}}
	}
	s := moiThietLap(t, tuyChon{})
	s.phien = conflict
	s.docPhien = func(id string) (int, map[string]any) {
		return 200, map[string]any{"id": id, "status": "open", "url": "https://pay.test/c/old", "expires_at": hetHanSau(s.dh, 5*time.Minute)}
	}
	kq := s.xuLy(yc{uri: "/premium/x", header: dieuHuong})
	if kq.PhanHoi.Status != 302 || kq.PhanHoi.Header.Get("Location") != "https://pay.test/c/old" {
		t.Fatalf("302 tới phiên cũ: %+v", kq.PhanHoi)
	}
	s.xuLy(yc{uri: "/premium/x", header: dieuHuong})
	if s.soPOST() != 1 {
		t.Fatalf("URL phiên cũ đệm ngắn hạn: %d POST", s.soPOST())
	}
	if s.soLoi() != 0 {
		t.Fatalf("không còn lỗi: %d", s.soLoi())
	}
	// Phiên cũ đã paid dù hub lỡ kèm url ⇒ KHÔNG đưa về, 402 không checkout_url, lỗi gốc vào BaoLoi.
	s2 := moiThietLap(t, tuyChon{})
	s2.phien = conflict
	s2.docPhien = func(id string) (int, map[string]any) {
		return 200, map[string]any{"id": id, "status": "paid", "url": "https://pay.test/c/old", "expires_at": hetHanSau(s2.dh, 5*time.Minute)}
	}
	kq = s2.xuLy(yc{uri: "/premium/x", header: api})
	if _, co := thanJSON(t, kq.PhanHoi)["checkout_url"]; co || s2.soLoi() != 1 {
		t.Fatalf("phiên đã trả không được đưa khách về: %s (lỗi %d)", kq.PhanHoi.Than, s2.soLoi())
	}
}
