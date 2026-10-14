// Package gopgoi gộp các lời gọi đồng thời cùng một khoá thành MỘT lần chạy (singleflight): chống bão khi bản đệm nguội.
package gopgoi

import "sync"

type lan[V any] struct {
	xong chan struct{}
	v    V
	err  error
}

// Nhom gộp theo khoá. Giá trị 0 dùng được.
type Nhom[V any] struct {
	mu  sync.Mutex
	bay map[string]*lan[V]
}

// Chay chạy `fn` cho `khoa`; lời gọi tới khi một lần chạy cùng khoá đang bay thì ĐỢI và nhận cùng kết quả. Khoá được giải phóng
// ngay khi `fn` xong (kể cả lỗi), nên lời gọi sau đó chạy lần mới. Trả thêm `chung` = true cho lời gọi nhập vào lần đang bay.
func (n *Nhom[V]) Chay(khoa string, fn func() (V, error)) (v V, err error, chung bool) {
	n.mu.Lock()
	if n.bay == nil {
		n.bay = map[string]*lan[V]{}
	}
	if l, ok := n.bay[khoa]; ok {
		n.mu.Unlock()
		<-l.xong
		return l.v, l.err, true
	}
	l := &lan[V]{xong: make(chan struct{})}
	n.bay[khoa] = l
	n.mu.Unlock()

	// Giải phóng cả khi `fn` panic: không để người đợi treo mãi.
	defer func() {
		n.mu.Lock()
		delete(n.bay, khoa)
		n.mu.Unlock()
		close(l.xong)
	}()
	l.v, l.err = fn()
	return l.v, l.err, false
}
