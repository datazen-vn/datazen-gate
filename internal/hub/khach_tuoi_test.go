package hub

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// payingHub: khách "none" cho tới khi `daTra` bật (mô phỏng hub đã ghi nhận thanh toán).
func payingHub(t *testing.T) (*hubGia, *atomic.Bool) {
	var daTra atomic.Bool
	h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) {
		if daTra.Load() {
			traSnap(w, snap("cus_1", "active", map[string]any{"features": map[string]bool{"premium": true}}), "")
			return
		}
		traSnap(w, snap("cus_1", "none", nil), "")
	})
	return h, &daTra
}

func TestDocTuoiBoQuaDemKhongQuyen(t *testing.T) {
	dh := &dongHo{t: goc}
	h, daTra := payingHub(t)
	k := khachThu(h, dh, nil)
	if lay(t, k, "cus_1", YeuCau{}).HieuLuc {
		t.Fatal("none")
	}
	daTra.Store(true)
	dh.tien(5 * time.Second) // bản none còn tươi (< 15 s)
	if lay(t, k, "cus_1", YeuCau{}).HieuLuc {
		t.Fatal("không tham số ⇒ vẫn theo bản đệm none")
	}
	kq := lay(t, k, "cus_1", YeuCau{Tuoi: true})
	if !kq.HieuLuc || kq.Nguon != NguonMang {
		t.Fatalf("đọc tươi phải thấy active từ hub: %+v", kq)
	}
	// Đọc tươi ghi đè đệm (none → active).
	if kq := lay(t, k, "cus_1", YeuCau{}); !kq.HieuLuc || kq.Nguon != NguonDem {
		t.Fatalf("sau đọc tươi đệm phải là active: %+v", kq)
	}
}

func TestDocTuoiCoTran1Lan5Giay(t *testing.T) {
	dh := &dongHo{t: goc}
	h, _ := payingHub(t)
	k := khachThu(h, dh, nil)
	lay(t, k, "cus_1", YeuCau{})
	for i := 0; i < 10; i++ {
		lay(t, k, "cus_1", YeuCau{Tuoi: true})
	}
	if h.soGoi() != 2 {
		t.Fatalf("10 lần quay về chỉ tốn 1 lần đọc tươi: %d lời gọi", h.soGoi())
	}
	dh.tien(TranDocTuoi)
	lay(t, k, "cus_1", YeuCau{Tuoi: true})
	if h.soGoi() != 3 {
		t.Fatalf("hết trần ⇒ được đọc tươi lại: %d", h.soGoi())
	}
}

func TestDocTuoiTranTinhTheoKhach(t *testing.T) {
	dh := &dongHo{t: goc}
	h := moiHubGia(t, func(_ int, r *http.Request, _ string, w http.ResponseWriter) {
		ref := "cus_1"
		if r.URL.Path == "/v1/customers/cus_2/entitlements" {
			ref = "cus_2"
		}
		traSnap(w, snap(ref, "none", nil), "")
	})
	k := khachThu(h, dh, nil)
	for _, ma := range []string{"cus_1", "cus_2"} {
		lay(t, k, ma, YeuCau{})
		lay(t, k, ma, YeuCau{Tuoi: true})
	}
	if h.soGoi() != 4 {
		t.Fatalf("trần là theo từng khách: %d", h.soGoi())
	}
}

func TestDocTuoiDemTrongCungDatMocTran(t *testing.T) {
	// README gate-v1 "Trần": MỌI lần gọi hub vì tham số đều đặt mốc, kể cả khi đệm trống.
	dh := &dongHo{t: goc}
	h, _ := payingHub(t)
	k := khachThu(h, dh, func(cf *CauHinh) { cf.TTL = 0 }) // đệm luôn trống
	lay(t, k, "cus_1", YeuCau{Tuoi: true})
	lay(t, k, "cus_1", YeuCau{Tuoi: true})
	if h.soGoi() != 2 {
		// TTL 0 ⇒ không có đệm nên lần 2 phải hỏi hub dù bị trần (không có gì để dùng) — đúng 2.
		t.Fatalf("mong 2 lời gọi (đệm tắt), có %d", h.soGoi())
	}
	h2, _ := payingHub(t)
	k2 := khachThu(h2, dh, nil)
	lay(t, k2, "cus_1", YeuCau{Tuoi: true}) // lạnh + có tham số: đặt mốc trần
	lay(t, k2, "cus_1", YeuCau{Tuoi: true}) // trong 5 s: dùng đệm vừa ghi
	if h2.soGoi() != 1 {
		t.Fatalf("lần lạnh đã đặt mốc trần ⇒ lần 2 dùng đệm: %d lời gọi", h2.soGoi())
	}
}

func TestDocTuoiLoiKhongLuiVeDemNoneConHan(t *testing.T) {
	dh := &dongHo{t: goc}
	var hong atomic.Bool
	h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) {
		if hong.Load() {
			traLoi(w, 503, "tam-thoi")
			return
		}
		traSnap(w, snap("cus_1", "none", nil), "")
	})
	k := khachThu(h, dh, nil) // stale_if_error = 0
	lay(t, k, "cus_1", YeuCau{})
	hong.Store(true)
	dh.tien(2 * time.Second)
	_, err := k.Lay(context.Background(), "cus_1", YeuCau{Tuoi: true})
	if l, ok := ALoi(err); !ok || l.Ma != LoiMayChu {
		t.Fatalf("đọc tươi hỏng phải là lỗi (503 trung thực), không lùi về đệm none còn hạn: %v", err)
	}
}

func TestDocTuoiLoiVoiStaleIfErrorBatThiCuu(t *testing.T) {
	dh := &dongHo{t: goc}
	var hong atomic.Bool
	h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) {
		if hong.Load() {
			traLoi(w, 503, "tam-thoi")
			return
		}
		traSnap(w, snap("cus_1", "active", nil), "")
	})
	k := khachThu(h, dh, func(cf *CauHinh) { cf.StaleIfError = time.Minute })
	lay(t, k, "cus_1", YeuCau{})
	hong.Store(true)
	dh.tien(time.Second)
	if kq := lay(t, k, "cus_1", YeuCau{Tuoi: true}); kq.Nguon != NguonCu {
		t.Fatalf("chỉ stale_if_error > 0 mới cứu: %+v", kq)
	}
}

func TestDocTuoiDongThoiGopMotLan(t *testing.T) {
	dh := &dongHo{t: goc}
	var dangVao atomic.Int32
	thaDi := make(chan struct{})
	h := moiHubGia(t, func(n int, _ *http.Request, _ string, w http.ResponseWriter) {
		if n == 1 {
			traSnap(w, snap("cus_1", "none", nil), "")
			return
		}
		dangVao.Add(1)
		<-thaDi
		traSnap(w, snap("cus_1", "active", nil), "")
	})
	k := khachThu(h, dh, nil)
	lay(t, k, "cus_1", YeuCau{})
	var wg sync.WaitGroup
	var thanhCong atomic.Int32
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if kq, err := k.Lay(context.Background(), "cus_1", YeuCau{Tuoi: true}); err == nil && kq.HieuLuc {
				thanhCong.Add(1)
			}
		}()
	}
	for dangVao.Load() < 1 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	close(thaDi)
	wg.Wait()
	if h.soGoi() != 2 || thanhCong.Load() != 6 {
		t.Fatalf("6 lần quay về đồng thời gộp thành 1 lần đọc tươi: %d lời gọi, %d thành công", h.soGoi(), thanhCong.Load())
	}
}

func TestDocTuoiKhongNhapVaoLanBayThuongBatDauTruoc(t *testing.T) {
	// Lần bay thường bắt đầu TRƯỚC khi khách trả có thể mang câu trả lời cũ ⇒ đọc tươi không nhập vào, tự chạy lần mới.
	dh := &dongHo{t: goc}
	vaoRoi, thaDi := make(chan struct{}), make(chan struct{})
	h := moiHubGia(t, func(n int, _ *http.Request, _ string, w http.ResponseWriter) {
		switch n {
		case 1:
			traSnap(w, snap("cus_1", "none", nil), "")
		case 2:
			close(vaoRoi)
			<-thaDi
			traSnap(w, snap("cus_1", "none", nil), "")
		default:
			traSnap(w, snap("cus_1", "active", nil), "")
		}
	})
	k := khachThu(h, dh, func(cf *CauHinh) { cf.TTL = 0 })
	lay(t, k, "cus_1", YeuCau{}) // lần 1
	go func() { _, _ = k.Lay(context.Background(), "cus_1", YeuCau{}) }()
	<-vaoRoi // lần bay thường đang treo
	kq := lay(t, k, "cus_1", YeuCau{Tuoi: true})
	close(thaDi)
	if !kq.HieuLuc {
		t.Fatalf("đọc tươi phải tự chạy lần mới và thấy active: %+v", kq)
	}
}
