package quyetdinh

import (
	"testing"
	"time"
)

func dem402(s *thietLap, goi int) {
	for i := 0; i < goi; i++ {
		s.xuLy(yc{uri: "/premium/x", header: api})
	}
}

func TestDemURLPhienToiReplaceAfterTru30s(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	s.phien = func(int) (int, map[string]any) {
		return 201, map[string]any{"id": "1", "url": "https://pay.test/c/abc", "expires_at": hetHanSau(s.dh, 30*time.Minute), "replace_after": hetHanSau(s.dh, 20*time.Minute)}
	}
	dem402(s, 3)
	if s.soPOST() != 1 {
		t.Fatalf("đệm: %d POST", s.soPOST())
	}
	s.dh.tien(1169 * time.Second) // còn 31 s tới replace_after
	dem402(s, 1)
	if s.soPOST() != 1 {
		t.Fatalf("còn 31 s ⇒ vẫn đệm: %d", s.soPOST())
	}
	s.dh.tien(2 * time.Second) // còn 29 s ⇒ hết đệm dù expires_at còn 10 phút
	dem402(s, 1)
	if s.soPOST() != 2 {
		t.Fatalf("qua replace_after − 30 s phải tạo/lấy lại: %d", s.soPOST())
	}
}

func TestDemURLPhienHubCuLuiVeHetHanTru11Phut(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	s.phien = func(int) (int, map[string]any) {
		return 201, map[string]any{"id": "1", "url": "https://pay.test/c/abc", "expires_at": hetHanSau(s.dh, 30*time.Minute)}
	}
	dem402(s, 1)
	s.dh.tien(19*time.Minute - time.Second) // còn 11 phút + 1 s
	dem402(s, 1)
	if s.soPOST() != 1 {
		t.Fatalf("%d", s.soPOST())
	}
	s.dh.tien(2 * time.Second) // còn 10 phút 59 s
	dem402(s, 1)
	if s.soPOST() != 2 {
		t.Fatalf("%d", s.soPOST())
	}
}

func TestPhienSapVaoVungBiThayKhongDuocDem(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	s.phien = func(int) (int, map[string]any) {
		return 201, map[string]any{"id": "1", "url": "https://pay.test/c/abc", "expires_at": hetHanSau(s.dh, 10*time.Minute)}
	}
	dem402(s, 2)
	if s.soPOST() != 2 {
		t.Fatalf("hub cũ + còn 10 phút: không đệm: %d", s.soPOST())
	}
	s2 := moiThietLap(t, tuyChon{})
	s2.phien = func(int) (int, map[string]any) {
		return 201, map[string]any{"id": "1", "url": "https://pay.test/c/abc", "expires_at": hetHanSau(s2.dh, 30*time.Minute), "replace_after": hetHanSau(s2.dh, 10*time.Second)}
	}
	dem402(s2, 2)
	if s2.soPOST() != 2 {
		t.Fatalf("replace_after đã (gần) qua: không đệm: %d", s2.soPOST())
	}
}

func TestGoiDongThoiChiTaoMotPhien(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	thaDi := make(chan struct{})
	vao := make(chan struct{}, 1)
	s.phien = func(int) (int, map[string]any) {
		select {
		case vao <- struct{}{}:
		default:
		}
		<-thaDi
		return 201, map[string]any{"id": "1", "url": "https://pay.test/c/abc", "expires_at": hetHanSau(s.dh, time.Hour), "replace_after": hetHanSau(s.dh, 50*time.Minute)}
	}
	xong := make(chan string, 5)
	for i := 0; i < 5; i++ {
		go func() {
			kq := s.xuLy(yc{uri: "/premium/x", header: dieuHuong})
			xong <- kq.PhanHoi.Header.Get("Location")
		}()
	}
	<-vao
	time.Sleep(100 * time.Millisecond)
	close(thaDi)
	for i := 0; i < 5; i++ {
		if loc := <-xong; loc != "https://pay.test/c/abc" {
			t.Fatalf("Location %q", loc)
		}
	}
	if s.soPOST() != 1 {
		t.Fatalf("5 yêu cầu đồng thời gộp thành 1 phiên: %d", s.soPOST())
	}
}

func TestQuenXoaPhienVaLoiCuaMoiGoi(t *testing.T) {
	s := moiThietLap(t, tuyChon{})
	dem402(s, 1)
	s.cong.Paywall.Quen("cus_1")
	dem402(s, 1)
	if s.soPOST() != 2 {
		t.Fatalf("Quen bỏ phiên đã đệm: %d", s.soPOST())
	}
	// Quen cũng xoá nhớ lỗi.
	s2 := moiThietLap(t, tuyChon{})
	s2.phien = func(int) (int, map[string]any) { return 500, map[string]any{} }
	dem402(s2, 2)
	s2.cong.Paywall.Quen("cus_1")
	dem402(s2, 1)
	if s2.soPOST() != 2 {
		t.Fatalf("Quen xoá nhớ lỗi: %d", s2.soPOST())
	}
}
