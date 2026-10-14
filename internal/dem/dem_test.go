package dem

import (
	"sync"
	"testing"
	"time"
)

type dongHo struct{ t time.Time }

func (d *dongHo) now() time.Time { return d.t }

func TestTuoiVaGiuThem(t *testing.T) {
	dh := &dongHo{time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)}
	d := Moi[string](10, time.Minute, dh.now)
	d.Dat("a", "1", dh.t.Add(10*time.Second))

	m, ok := d.Lay("a")
	if !ok || !d.Tuoi(m) || m.GiaTri != "1" {
		t.Fatalf("mong mục tươi, có %+v ok=%v", m, ok)
	}
	dh.t = dh.t.Add(30 * time.Second) // quá tươi, còn trong thời gian giữ ⇒ dùng được để hỏi lại bằng ETag
	m, ok = d.Lay("a")
	if !ok || d.Tuoi(m) {
		t.Fatalf("mong mục quá tươi nhưng còn giữ, có %+v ok=%v", m, ok)
	}
	dh.t = dh.t.Add(41 * time.Second) // 10:01:11 > TuoiDen (10:00:10) + 1 phút ⇒ bỏ hẳn
	if _, ok = d.Lay("a"); ok {
		t.Fatal("mục phải bị bỏ sau thời gian giữ")
	}
	if d.Dem() != 0 {
		t.Fatalf("mục hết hạn phải bị xoá, còn %d", d.Dem())
	}
}

func TestFIFOKhiDay(t *testing.T) {
	d := Moi[int](3, 0, nil)
	h := time.Now().Add(time.Hour)
	for i, k := range []string{"a", "b", "c"} {
		d.Dat(k, i, h)
	}
	d.Dat("a", 9, h) // chèn lại ⇒ a thành mới nhất, b cũ nhất
	d.Dat("d", 3, h)
	if _, ok := d.Lay("b"); ok {
		t.Fatal("b (chèn sớm nhất) phải bị bỏ")
	}
	for _, k := range []string{"a", "c", "d"} {
		if _, ok := d.Lay(k); !ok {
			t.Fatalf("%s phải còn", k)
		}
	}
	if m, _ := d.Lay("a"); m.GiaTri != 9 {
		t.Fatalf("a phải là giá trị mới, có %d", m.GiaTri)
	}
}

func TestXoa(t *testing.T) {
	d := Moi[int](3, 0, nil)
	d.Dat("a", 1, time.Now().Add(time.Hour))
	d.Xoa("a")
	d.Xoa("khong-co")
	if _, ok := d.Lay("a"); ok {
		t.Fatal("a phải bị xoá")
	}
}

func TestDongThoi(t *testing.T) {
	d := Moi[int](50, time.Minute, nil)
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 500; i++ {
				k := string(rune('a' + i%80))
				d.Dat(k, i, time.Now().Add(time.Second))
				d.Lay(k)
				if i%7 == 0 {
					d.Xoa(k)
				}
			}
		}()
	}
	wg.Wait()
	if d.Dem() > 50 {
		t.Fatalf("vượt trần: %d", d.Dem())
	}
}
