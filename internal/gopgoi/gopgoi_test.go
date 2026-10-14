package gopgoi

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestGopDongThoiChiChayMotLan(t *testing.T) {
	var n Nhom[int]
	var soLan, chung, sanSang atomic.Int32
	thaDi := make(chan struct{})
	var wg sync.WaitGroup
	const soGoi = 20
	for i := 0; i < soGoi; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sanSang.Add(1)
			v, err, c := n.Chay("k", func() (int, error) {
				soLan.Add(1)
				<-thaDi
				return 42, nil
			})
			if v != 42 || err != nil {
				t.Errorf("v=%d err=%v", v, err)
			}
			if c {
				chung.Add(1)
			}
		}()
	}
	// Mọi goroutine đã sẵn sàng; chờ chút cho chúng nhập vào lần đang bay rồi mới thả.
	for sanSang.Load() < soGoi {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	close(thaDi)
	wg.Wait()
	if soLan.Load() != 1 || chung.Load() != soGoi-1 {
		t.Fatalf("mong 1 lần chạy + %d nhập vào, có %d chạy + %d nhập vào", soGoi-1, soLan.Load(), chung.Load())
	}
}

func TestLoiDuocChiaVaKhoaGiaiPhong(t *testing.T) {
	var n Nhom[string]
	loi := errors.New("hub chết")
	_, err, _ := n.Chay("k", func() (string, error) { return "", loi })
	if !errors.Is(err, loi) {
		t.Fatalf("err = %v", err)
	}
	v, err, chung := n.Chay("k", func() (string, error) { return "lan-2", nil })
	if v != "lan-2" || err != nil || chung {
		t.Fatalf("lần sau phải chạy mới: %q %v %v", v, err, chung)
	}
}

func TestPanicKhongTreoNguoiDoi(t *testing.T) {
	var n Nhom[int]
	func() {
		defer func() { _ = recover() }()
		_, _, _ = n.Chay("k", func() (int, error) { panic("boom") })
	}()
	v, _, chung := n.Chay("k", func() (int, error) { return 7, nil })
	if v != 7 || chung {
		t.Fatalf("khoá phải được giải phóng sau panic: %d %v", v, chung)
	}
}
