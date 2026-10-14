// Package dem là bộ nhớ đệm trong tiến trình có hạn "tươi", FIFO khi đầy. Đơn giản có chủ ý (không LRU): đủ để chặn rò bộ nhớ
// khi có rất nhiều khách.
//
// Mục KHÔNG bị xoá ngay khi quá hạn "tươi" (TuoiDen): nó còn dùng được để hỏi lại hub bằng If-None-Match (304 ⇒ gia hạn) và làm bản
// dự phòng stale-if-error. Người gọi tự so TuoiDen với giờ hiện tại. Mục bị bỏ hẳn sau TuoiDen + giữ thêm.
package dem

import (
	"container/list"
	"sync"
	"time"
)

// Muc là một mục đệm.
type Muc[V any] struct {
	GiaTri V
	// TuoiDen: tới mốc này mục còn "tươi" (dùng thẳng không cần hỏi hub).
	TuoiDen time.Time
}

type phanTu[V any] struct {
	khoa string
	muc  Muc[V]
}

// Dem an toàn cho nhiều goroutine.
type Dem[V any] struct {
	mu      sync.Mutex
	muc     map[string]*list.Element
	thuTu   *list.List // đầu = chèn sớm nhất
	toiDa   int
	giuThem time.Duration
	now     func() time.Time
}

// Moi tạo bộ đệm: `toiDa` mục (vượt thì bỏ mục chèn sớm nhất), giữ mục thêm `giuThem` sau TuoiDen. `now` nil ⇒ time.Now.
func Moi[V any](toiDa int, giuThem time.Duration, now func() time.Time) *Dem[V] {
	if now == nil {
		now = time.Now
	}
	return &Dem[V]{muc: map[string]*list.Element{}, thuTu: list.New(), toiDa: toiDa, giuThem: giuThem, now: now}
}

// Lay trả mục (tươi hoặc đã quá hạn nhưng còn trong thời gian giữ); mục quá thời gian giữ bị xoá và coi như không có.
func (d *Dem[V]) Lay(khoa string) (Muc[V], bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	e, ok := d.muc[khoa]
	if !ok {
		return Muc[V]{}, false
	}
	m := e.Value.(*phanTu[V]).muc
	if !d.now().Before(m.TuoiDen.Add(d.giuThem)) {
		d.xoaLocked(khoa, e)
		return Muc[V]{}, false
	}
	return m, true
}

// Tuoi: mục còn tươi tại thời điểm hiện tại không.
func (d *Dem[V]) Tuoi(m Muc[V]) bool { return d.now().Before(m.TuoiDen) }

// Dat ghi (hoặc chèn lại, cập nhật thứ tự) một mục.
func (d *Dem[V]) Dat(khoa string, v V, tuoiDen time.Time) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if e, ok := d.muc[khoa]; ok {
		d.xoaLocked(khoa, e)
	}
	d.muc[khoa] = d.thuTu.PushBack(&phanTu[V]{khoa: khoa, muc: Muc[V]{GiaTri: v, TuoiDen: tuoiDen}})
	for d.thuTu.Len() > d.toiDa {
		dau := d.thuTu.Front()
		d.xoaLocked(dau.Value.(*phanTu[V]).khoa, dau)
	}
}

// Xoa bỏ một mục (không có thì thôi).
func (d *Dem[V]) Xoa(khoa string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if e, ok := d.muc[khoa]; ok {
		d.xoaLocked(khoa, e)
	}
}

// Dem trả số mục hiện có.
func (d *Dem[V]) Dem() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.thuTu.Len()
}

func (d *Dem[V]) xoaLocked(khoa string, e *list.Element) {
	d.thuTu.Remove(e)
	delete(d.muc, khoa)
}
