package hub

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func lay(t *testing.T, k *Khach, ma string, y YeuCau) *KetQua {
	t.Helper()
	kq, err := k.Lay(context.Background(), ma, y)
	if err != nil {
		t.Fatalf("Lay(%s): %v", ma, err)
	}
	return kq
}

// hubTheoKhach trả snapshot theo hàm `f(n)` (n = lần gọi, từ 1).
func hubTheoLan(t *testing.T, f func(n int) (body map[string]any, etag string)) *hubGia {
	return moiHubGia(t, func(n int, r *http.Request, _ string, w http.ResponseWriter) {
		b, etag := f(n)
		traSnap(w, b, etag)
	})
}

func TestDemCoQuyenTheoTTLVaNguonDem(t *testing.T) {
	dh := &dongHo{t: goc}
	h := hubTheoLan(t, func(int) (map[string]any, string) {
		return snap("cus_1", "active", map[string]any{"plan_code": "pro", "features": map[string]bool{"premium": true}}), `W/"1:active"`
	})
	k := khachThu(h, dh, nil)
	if kq := lay(t, k, "cus_1", YeuCau{}); kq.Nguon != NguonMang || !kq.HieuLuc || kq.MaGoi != "pro" {
		t.Fatalf("lần đầu: %+v", kq)
	}
	dh.tien(59 * time.Second)
	if kq := lay(t, k, "cus_1", YeuCau{}); kq.Nguon != NguonDem {
		t.Fatalf("còn trong TTL phải từ cache: %+v", kq)
	}
	if h.soGoi() != 1 {
		t.Fatalf("1 lời gọi hub, có %d", h.soGoi())
	}
	dh.tien(2 * time.Second) // 61 s: hỏi lại bằng ETag
	lay(t, k, "cus_1", YeuCau{})
	if h.soGoi() != 2 {
		t.Fatalf("quá TTL phải hỏi lại: %d", h.soGoi())
	}
}

func TestKhongCoQuyenChiDem15Giay(t *testing.T) {
	dh := &dongHo{t: goc}
	h := hubTheoLan(t, func(n int) (map[string]any, string) {
		if n == 1 {
			return snap("cus_1", "none", nil), `W/"1:none"`
		}
		return snap("cus_1", "active", map[string]any{"features": map[string]bool{"premium": true}}), `W/"2:active"`
	})
	k := khachThu(h, dh, nil) // TTL 60 s
	if kq := lay(t, k, "cus_1", YeuCau{}); kq.HieuLuc {
		t.Fatal("none không có quyền")
	}
	dh.tien(5 * time.Second)
	if kq := lay(t, k, "cus_1", YeuCau{}); kq.HieuLuc || kq.Nguon != NguonDem {
		t.Fatalf("5 s sau vẫn dùng đệm none: %+v", kq)
	}
	dh.tien(11 * time.Second) // 16 s > 15 s dù TTL 60 s
	if kq := lay(t, k, "cus_1", YeuCau{}); !kq.HieuLuc || kq.Nguon != NguonMang {
		t.Fatalf("quá 15 s phải hỏi lại và thấy active: %+v", kq)
	}
}

func TestDemKhongQuaMocHetKyHoacAnHan(t *testing.T) {
	dh := &dongHo{t: goc}
	het := goc.Add(20 * time.Second).Format(time.RFC3339)
	h := hubTheoLan(t, func(int) (map[string]any, string) {
		return snap("cus_1", "active", map[string]any{"current_period_end": het}), ""
	})
	k := khachThu(h, dh, nil)
	lay(t, k, "cus_1", YeuCau{})
	dh.tien(19 * time.Second)
	lay(t, k, "cus_1", YeuCau{})
	if h.soGoi() != 1 {
		t.Fatalf("còn 1 s tới hết kỳ vẫn dùng đệm: %d", h.soGoi())
	}
	dh.tien(2 * time.Second) // qua current_period_end (20 s) dù TTL 60 s
	lay(t, k, "cus_1", YeuCau{})
	if h.soGoi() != 2 {
		t.Fatalf("qua mốc hết kỳ phải hỏi lại: %d", h.soGoi())
	}
}

func TestAnHanTinhTheoDongHo(t *testing.T) {
	dh := &dongHo{t: goc}
	h := hubTheoLan(t, func(int) (map[string]any, string) {
		return snap("cus_1", "past_due", map[string]any{"features": map[string]bool{"premium": true}, "grace_until": goc.Add(10 * time.Second).Format(time.RFC3339)}), ""
	})
	k := khachThu(h, dh, nil)
	kq := lay(t, k, "cus_1", YeuCau{TinhNang: "premium"})
	if !kq.HieuLuc || !kq.AnHan || kq.TinhNang == nil || !kq.TinhNang.ChoPhep {
		t.Fatalf("trong ân hạn: %+v", kq)
	}
	dh.tien(11 * time.Second)
	kq = lay(t, k, "cus_1", YeuCau{TinhNang: "premium"})
	if kq.HieuLuc || kq.AnHan || len(kq.Features) != 0 {
		t.Fatalf("qua ân hạn: không còn quyền, không rò features: %+v", kq)
	}
}

func TestHetHanChoPhepVaGioiHanKhong(t *testing.T) {
	dh := &dongHo{t: goc}
	h := hubTheoLan(t, func(int) (map[string]any, string) {
		return snap("cus_1", "active", map[string]any{
			"features": map[string]bool{"a": true, "b": true, "c": false},
			"limits":   map[string]any{"b": map[string]any{"value": 0}},
		}), ""
	})
	k := khachThu(h, dh, nil)
	cacTruongHop := map[string]bool{"a": true, "b": false, "c": false, "vang": false}
	for tinhNang, mong := range cacTruongHop {
		kq := lay(t, k, "cus_1", YeuCau{TinhNang: tinhNang})
		if kq.TinhNang.ChoPhep != mong {
			t.Errorf("tính năng %s: ChoPhep = %v, mong %v", tinhNang, kq.TinhNang.ChoPhep, mong)
		}
	}
}

func TestDapHoiBangETagVa304(t *testing.T) {
	dh := &dongHo{t: goc}
	h := moiHubGia(t, func(n int, r *http.Request, _ string, w http.ResponseWriter) {
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.Header().Set("ETag", `"v1b"`) // 304 có thể kèm ETag mới
			w.WriteHeader(http.StatusNotModified)
			return
		}
		traSnap(w, snap("cus_1", "active", nil), `"v1"`)
	})
	k := khachThu(h, dh, nil)
	lay(t, k, "cus_1", YeuCau{})
	dh.tien(61 * time.Second)
	kq := lay(t, k, "cus_1", YeuCau{})
	if kq.Nguon != NguonMang || !kq.HieuLuc {
		t.Fatalf("304 gia hạn bản cũ: %+v", kq)
	}
	if got := h.cuoi().header.Get("If-None-Match"); got != `"v1"` {
		t.Fatalf("phải gửi If-None-Match: %q", got)
	}
	dh.tien(30 * time.Second) // vừa được gia hạn ⇒ lại dùng đệm
	lay(t, k, "cus_1", YeuCau{})
	if h.soGoi() != 2 {
		t.Fatalf("sau 304 TTL phải được làm mới: %d lời gọi", h.soGoi())
	}
	dh.tien(40 * time.Second)
	lay(t, k, "cus_1", YeuCau{})
	if got := h.cuoi().header.Get("If-None-Match"); got != `"v1b"` {
		t.Fatalf("ETag mới từ 304 phải được dùng: %q", got)
	}
}

func Test304KhiKhongGiuBanNaoLaLoi(t *testing.T) {
	dh := &dongHo{t: goc}
	// Hub trả 304 dù ta không gửi If-None-Match: gate Go coi như 3xx lạ (không theo) ⇒ lỗi.
	h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) { w.WriteHeader(http.StatusNotModified) })
	_, err := khachThu(h, dh, nil).Lay(context.Background(), "cus_1", YeuCau{})
	if l, ok := ALoi(err); !ok || l.Ma != LoiChuyenHuong {
		t.Fatalf("mong dz_redirect_refused, có %v", err)
	}
}

func TestFailClosedVaLoiHaTang(t *testing.T) {
	dh := &dongHo{t: goc}
	for _, tt := range []int{500, 502, 503} {
		h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) { traLoi(w, tt, "loi-may-chu") })
		_, err := khachThu(h, dh, nil).Lay(context.Background(), "cus_1", YeuCau{})
		l, ok := ALoi(err)
		if !ok || l.Ma != LoiMayChu || !l.HaTang() || l.HubCode != "loi-may-chu" {
			t.Errorf("%d: mong dz_server_error hạ tầng, có %v", tt, err)
		}
	}
}

func TestFailOpenChiCuuLoiHaTang(t *testing.T) {
	dh := &dongHo{t: goc}
	cacTruongHop := []struct {
		tt        int
		moCua     bool
		maLoiMong string
	}{
		{500, true, ""}, // được cứu
		{502, true, ""},
		{401, false, LoiKhoa}, // khoá bị thu hồi: fail-open ở đây là mở cửa trả phí
		{403, false, LoiTrangThai},
		{404, false, LoiTrangThai}, // hub thật KHÔNG 404 cho ref lạ ⇒ sai đường/sai app
		{429, false, LoiHanMuc},    // quá tải hub không được thành nút mở cổng
	}
	for _, c := range cacTruongHop {
		h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) { traLoi(w, c.tt, "x") })
		var daBao atomic.Int32
		k := khachThu(h, dh, func(cf *CauHinh) { cf.OnLoi = func(*Loi) { daBao.Add(1) } })
		kq, err := k.Lay(context.Background(), "cus_1", YeuCau{CheDoLoi: "open", TinhNang: "premium"})
		if c.moCua {
			if err != nil || kq.Nguon != NguonMoCua || !kq.HieuLuc || !kq.TinhNang.ChoPhep || kq.Loi == nil {
				t.Errorf("%d: mong fail-open, có %+v %v", c.tt, kq, err)
			}
			if daBao.Load() != 1 {
				t.Errorf("%d: lỗi bị cho qua phải được báo đúng 1 lần, có %d", c.tt, daBao.Load())
			}
			continue
		}
		if l, ok := ALoi(err); !ok || l.Ma != c.maLoiMong {
			t.Errorf("%d: mong %s dù fail_mode open, có %v", c.tt, c.maLoiMong, err)
		}
	}
}

func TestChuyenHuongBiTuChoi(t *testing.T) {
	dh := &dongHo{t: goc}
	var theoDen atomic.Int32
	dich := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) { theoDen.Add(1) })
	h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) {
		http.Redirect(w, &http.Request{}, dich.URL+"/x", http.StatusFound)
	})
	k := khachThu(h, dh, nil)
	_, err := k.Lay(context.Background(), "cus_1", YeuCau{CheDoLoi: "open"})
	if l, ok := ALoi(err); !ok || l.Ma != LoiChuyenHuong || l.HaTang() {
		t.Fatalf("3xx là lỗi cấu hình, không phải hạ tầng: %v", err)
	}
	if theoDen.Load() != 0 {
		t.Fatal("không được theo chuyển hướng (khoá sẽ sang host khác)")
	}
}

func TestStaleIfErrorVaKhongVuotMoc(t *testing.T) {
	dh := &dongHo{t: goc}
	het := goc.Add(2 * time.Minute).Format(time.RFC3339)
	var hong atomic.Bool
	h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) {
		if hong.Load() {
			traLoi(w, 503, "tam-thoi")
			return
		}
		traSnap(w, snap("cus_1", "active", map[string]any{"current_period_end": het}), "")
	})
	k := khachThu(h, dh, func(cf *CauHinh) { cf.StaleIfError = 5 * time.Minute })
	lay(t, k, "cus_1", YeuCau{})
	hong.Store(true)
	dh.tien(90 * time.Second) // quá TTL 60 s, chưa tới hết kỳ (120 s)
	kq := lay(t, k, "cus_1", YeuCau{})
	if kq.Nguon != NguonCu || !kq.HieuLuc || kq.Loi == nil {
		t.Fatalf("hub lỗi ⇒ dùng bản cũ: %+v", kq)
	}
	dh.tien(40 * time.Second) // qua current_period_end: bản cũ KHÔNG được vượt mốc của chính nó
	if _, err := k.Lay(context.Background(), "cus_1", YeuCau{}); err == nil {
		t.Fatal("qua mốc hết kỳ mà hub lỗi phải là lỗi thật, không phải 'vẫn active từ cache'")
	}
}

func TestStaleKhongCuuLoi4xx(t *testing.T) {
	dh := &dongHo{t: goc}
	var ma atomic.Int32
	ma.Store(200)
	h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) {
		if tt := int(ma.Load()); tt != 200 {
			traLoi(w, tt, "x")
			return
		}
		traSnap(w, snap("cus_1", "active", nil), "")
	})
	k := khachThu(h, dh, func(cf *CauHinh) { cf.StaleIfError = time.Hour })
	lay(t, k, "cus_1", YeuCau{})
	dh.tien(2 * time.Minute)
	for _, tt := range []int{401, 403, 404, 429} {
		ma.Store(int32(tt))
		if _, err := k.Lay(context.Background(), "cus_1", YeuCau{}); err == nil {
			t.Errorf("%d không được che bằng bản cũ", tt)
		}
	}
}

func TestGopLoiGoiDongThoi(t *testing.T) {
	dh := &dongHo{t: goc}
	thaDi := make(chan struct{})
	var dangVao atomic.Int32
	h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) {
		dangVao.Add(1)
		<-thaDi
		traSnap(w, snap("cus_1", "active", nil), "")
	})
	k := khachThu(h, dh, nil)
	const n = 30
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if kq, err := k.Lay(context.Background(), "cus_1", YeuCau{}); err != nil || !kq.HieuLuc {
				t.Errorf("Lay: %v", err)
			}
		}()
	}
	for dangVao.Load() < 1 {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	close(thaDi)
	wg.Wait()
	if h.soGoi() != 1 {
		t.Fatalf("%d lời gọi đồng thời phải gộp thành 1 lời gọi hub, có %d", n, h.soGoi())
	}
}

func TestHuyBoBanDemVaGhiDemCuaLoiGoiDangBay(t *testing.T) {
	dh := &dongHo{t: goc}
	vaoRoi, thaDi := make(chan struct{}), make(chan struct{})
	h := moiHubGia(t, func(n int, _ *http.Request, _ string, w http.ResponseWriter) {
		if n == 1 {
			close(vaoRoi)
			<-thaDi
			traSnap(w, snap("cus_1", "none", nil), "") // câu trả lời CŨ của lần bay bắt đầu trước webhook
			return
		}
		traSnap(w, snap("cus_1", "active", nil), "")
	})
	k := khachThu(h, dh, nil)
	xong := make(chan *KetQua)
	go func() { xong <- lay(t, k, "cus_1", YeuCau{}) }()
	<-vaoRoi
	k.Huy("cus_1") // webhook báo đổi trong lúc lần bay đang chạy
	close(thaDi)
	if kq := <-xong; kq.HieuLuc {
		t.Fatalf("lần bay cũ trả none: %+v", kq)
	}
	// Câu trả lời cũ KHÔNG được ghi đệm: lần sau phải hỏi lại và thấy active.
	if kq := lay(t, k, "cus_1", YeuCau{}); !kq.HieuLuc || kq.Nguon != NguonMang {
		t.Fatalf("bản cũ không được sống lại sau Huy: %+v", kq)
	}
}

func TestMaKhachTrongDuongDanDuocMaHoa(t *testing.T) {
	dh := &dongHo{t: goc}
	cacMa := []string{"cus_1", "a/b", "a?b#c%", "..a", "a.b", "%2e%2e", "x+y@z.vn", "'()*!~"}
	h := moiHubGia(t, func(_ int, r *http.Request, _ string, w http.ResponseWriter) {
		// Phản hồi theo ref hub NHẬN ĐƯỢC (đã giải mã) để kiểm khớp customer_ref.
		ref := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/customers/"), "/entitlements")
		traSnap(w, snap(ref, "none", nil), "")
	})
	k := khachThu(h, dh, nil)
	for _, ma := range cacMa {
		if _, err := k.Lay(context.Background(), ma, YeuCau{}); err != nil {
			t.Errorf("ref %q: %v", ma, err)
			continue
		}
		duong := h.cuoi().duongDanTho
		if strings.Count(duong, "/") != 4 { // /v1/customers/{ref}/entitlements: ref không được chứa `/` thô
			t.Errorf("ref %q: đường dẫn %q để lộ ký tự đặc biệt", ma, duong)
		}
	}
}

func TestMaKhachSaiBiTuChoiTaiCho(t *testing.T) {
	dh := &dongHo{t: goc}
	h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) { traSnap(w, snap("x", "none", nil), "") })
	k := khachThu(h, dh, nil)
	for _, ma := range []string{"", ".", "..", "co khoang trang", "tab\t", strings.Repeat("a", 201), "tiếng-việt"} {
		_, err := k.Lay(context.Background(), ma, YeuCau{})
		if l, ok := ALoi(err); !ok || l.Ma != LoiMaKhach {
			t.Errorf("ref %q phải bị từ chối: %v", ma, err)
		}
		if strings.Contains(err.Error(), ma) && ma != "" {
			t.Errorf("thông báo lỗi lộ giá trị ref: %v", err)
		}
	}
	if h.soGoi() != 0 {
		t.Fatal("ref sai không được chạm hub")
	}
}

func TestHeaderGuiHubKhongMangDuLieuKhach(t *testing.T) {
	dh := &dongHo{t: goc}
	h := hubTheoLan(t, func(int) (map[string]any, string) { return snap("cus_1", "none", nil), "" })
	k := khachThu(h, dh, nil)
	lay(t, k, "cus_1", YeuCau{})
	hd := h.cuoi().header
	if hd.Get("Authorization") != "Bearer dz_test_khoa" || hd.Get("User-Agent") != "dz-gate/test" {
		t.Fatalf("header: %v", hd)
	}
	for _, ten := range []string{"Cookie", "X-Forwarded-For", "X-Dz-Customer-Ref"} {
		if hd.Get(ten) != "" {
			t.Errorf("không được gửi %s tới hub", ten)
		}
	}
}

func TestThanLonVaHetGio(t *testing.T) {
	dh := &dongHo{t: goc}
	h := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"customer_ref":"cus_1","status":"none","pad":"` + strings.Repeat("x", 300<<10) + `"}`))
	})
	_, err := khachThu(h, dh, nil).Lay(context.Background(), "cus_1", YeuCau{})
	if l, ok := ALoi(err); !ok || l.Ma != LoiPhanHoi {
		t.Fatalf("thân > 256 KB phải là dz_bad_response: %v", err)
	}

	cham := moiHubGia(t, func(_ int, _ *http.Request, _ string, w http.ResponseWriter) { time.Sleep(300 * time.Millisecond) })
	k := khachThu(cham, dh, func(cf *CauHinh) { cf.Timeout = 50 * time.Millisecond })
	_, err = k.Lay(context.Background(), "cus_1", YeuCau{})
	if l, ok := ALoi(err); !ok || l.Ma != LoiHetGio || !l.HaTang() {
		t.Fatalf("quá hạn phải là dz_timeout hạ tầng: %v", err)
	}
}

func TestTrangThaiLaBaoLoiVaKhongCoQuyen(t *testing.T) {
	dh := &dongHo{t: goc}
	var baoLoi atomic.Int32
	h := hubTheoLan(t, func(int) (map[string]any, string) {
		return snap("cus_1", "paused", map[string]any{"features": map[string]bool{"premium": true}}), ""
	})
	k := khachThu(h, dh, func(cf *CauHinh) { cf.OnLoi = func(*Loi) { baoLoi.Add(1) } })
	kq := lay(t, k, "cus_1", YeuCau{TinhNang: "premium"})
	if kq.TrangThai != TrangLa || kq.HieuLuc || kq.Loi == nil || kq.TinhNang.ChoPhep || baoLoi.Load() != 1 {
		t.Fatalf("status lạ: %+v (báo lỗi %d)", kq, baoLoi.Load())
	}
}

func TestKhongCoDemKhiTTLBang0(t *testing.T) {
	dh := &dongHo{t: goc}
	h := hubTheoLan(t, func(int) (map[string]any, string) { return snap("cus_1", "active", nil), "" })
	k := khachThu(h, dh, func(cf *CauHinh) { cf.TTL = 0 })
	lay(t, k, "cus_1", YeuCau{})
	lay(t, k, "cus_1", YeuCau{})
	if h.soGoi() != 2 {
		t.Fatalf("TTL 0 = tắt đệm: %d lời gọi", h.soGoi())
	}
}
