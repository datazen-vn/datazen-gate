package quyetdinh

import (
	"testing"
	"time"

	"github.com/datazen-vn/datazen-gate/internal/hub"
	"github.com/datazen-vn/datazen-gate/internal/tuyen"
)

func mot(v int64) *int64 { return &v }

func TestQuyetQuyen(t *testing.T) {
	het := goc.Add(time.Hour)
	cacTruongHop := []struct {
		ten    string
		kq     hub.KetQua
		route  Route
		cho    bool
		lyDo   string
		status int
		anHan  bool
	}{
		{"active", hub.KetQua{TrangThai: hub.Active, HieuLuc: true}, Route{}, true, "", 0, false},
		{"ân hạn", hub.KetQua{TrangThai: hub.PastDue, HieuLuc: true, AnHan: true}, Route{}, true, "", 0, true},
		{"past_due hết ân hạn", hub.KetQua{TrangThai: hub.PastDue, HetAnHan: &het}, Route{}, false, LyDoKhongCoQuyen, 402, false},
		{"expired", hub.KetQua{TrangThai: hub.Expired}, Route{}, false, LyDoKhongCoQuyen, 402, false},
		{"cancelled", hub.KetQua{TrangThai: hub.Cancelled}, Route{}, false, LyDoKhongCoQuyen, 402, false},
		{"none", hub.KetQua{TrangThai: hub.None}, Route{}, false, LyDoKhongCoQuyen, 402, false},
		{"status lạ ⇒ 503 không 402", hub.KetQua{TrangThai: hub.TrangLa}, Route{}, false, LyDoKhongSan, 503, false},
		{"fail-open ⇒ cho", hub.KetQua{TrangThai: hub.TrangLa, HieuLuc: true, Nguon: hub.NguonMoCua}, Route{TinhNang: "x"}, true, "", 0, false},
		{"feature bật", hub.KetQua{TrangThai: hub.Active, HieuLuc: true, TinhNang: &hub.NhinTinhNang{ChoPhep: true}}, Route{TinhNang: "x"}, true, "", 0, false},
		{"feature tắt dù active", hub.KetQua{TrangThai: hub.Active, HieuLuc: true, TinhNang: &hub.NhinTinhNang{}}, Route{TinhNang: "x"}, false, LyDoKhongCoQuyen, 402, false},
		{"feature giới hạn 0", hub.KetQua{TrangThai: hub.Active, HieuLuc: true, TinhNang: &hub.NhinTinhNang{BatTat: true, GioiHan: mot(0)}}, Route{TinhNang: "x"}, false, LyDoKhongCoQuyen, 402, false},
		{"yêu cầu feature mà không có nhìn feature", hub.KetQua{TrangThai: hub.Active, HieuLuc: true}, Route{TinhNang: "x"}, false, LyDoKhongCoQuyen, 402, false},
	}
	for _, c := range cacTruongHop {
		kq := c.kq
		if kq.Nguon == "" {
			kq.Nguon = hub.NguonMang
		}
		q := QuyetQuyen(&kq, c.route)
		if q.Cho != c.cho || q.LyDo != c.lyDo || q.Status != c.status || q.AnHan != c.anHan {
			t.Errorf("%s: %+v", c.ten, q)
		}
	}
}

func TestTuDongDong(t *testing.T) {
	if r := TuDong(nil); r != (Route{}) {
		t.Fatalf("nil = tuyến mặc định: %+v", r)
	}
	r := TuDong(&tuyen.Dong{HanhDong: tuyen.Mem, TinhNang: "f", MaGoi: "team", CheDoLoi: "open"})
	if !r.Mem || r.TinhNang != "f" || r.MaGoi != "team" || r.CheDoLoi != "open" {
		t.Fatalf("%+v", r)
	}
	if TuDong(&tuyen.Dong{HanhDong: tuyen.Can}).Mem {
		t.Fatal("require không phải soft")
	}
}
