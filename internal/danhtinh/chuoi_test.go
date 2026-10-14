package danhtinh

import (
	"context"
	"net/http"
	"testing"
)

type boGia struct {
	ma  string
	err error
	goi int
}

func (b *boGia) NhanDien(context.Context, *http.Request) (string, error) {
	b.goi++
	return b.ma, b.err
}

func TestChuoiGiaiQuyet(t *testing.T) {
	loiMang := &LoiHaTang{ChiTiet: "hook chết"}
	cacTruongHop := []struct {
		ten      string
		bo       []*boGia
		mongLoai Loai
		mongMa   string
		daGoi    []int // số lần mỗi bo bị gọi
		soBao    int
	}{
		{"resolver đầu nhận ra ⇒ dừng", []*boGia{{ma: "cus_1"}, {ma: "cus_2"}}, CoKhach, "cus_1", []int{1, 0}, 0},
		{"rỗng ⇒ thử kế", []*boGia{{}, {ma: "cus_2"}}, CoKhach, "cus_2", []int{1, 1}, 0},
		{"không ai nhận ra ⇒ chưa đăng nhập", []*boGia{{}, {}}, ChuaDangNhap, "", []int{1, 1}, 0},
		{"token sai ⇒ coi như không nhận ra, thử tiếp", []*boGia{{err: ErrTokenSai}, {ma: "cus_2"}}, CoKhach, "cus_2", []int{1, 1}, 1},
		{"chỉ token sai ⇒ chưa đăng nhập (không phải 503)", []*boGia{{err: ErrTokenSai}}, ChuaDangNhap, "", []int{1}, 1},
		{"lỗi hạ tầng rồi hết chuỗi ⇒ KHÔNG BIẾT", []*boGia{{err: loiMang}, {}}, KhongBiet, "", []int{1, 1}, 1},
		{"lỗi hạ tầng nhưng resolver kế nhận ra ⇒ có khách", []*boGia{{err: loiMang}, {ma: "cus_2"}}, CoKhach, "cus_2", []int{1, 1}, 1},
		{"ref sai hình ⇒ chưa đăng nhập, DỪNG chuỗi", []*boGia{{ma: ".."}, {ma: "cus_2"}}, ChuaDangNhap, "", []int{1, 0}, 1},
		{"ref có khoảng trắng ⇒ chưa đăng nhập", []*boGia{{ma: "a b"}}, ChuaDangNhap, "", []int{1}, 1},
	}
	for _, c := range cacTruongHop {
		var bos []Bo
		for _, b := range c.bo {
			bos = append(bos, b)
		}
		soBao := 0
		kq := GiaiQuyet(context.Background(), &http.Request{}, bos, func(error) { soBao++ })
		if kq.Loai != c.mongLoai || kq.MaKhach != c.mongMa || soBao != c.soBao {
			t.Errorf("%s: %+v (báo %d), mong loại %v mã %q báo %d", c.ten, kq, soBao, c.mongLoai, c.mongMa, c.soBao)
		}
		for i, b := range c.bo {
			if b.goi != c.daGoi[i] {
				t.Errorf("%s: resolver %d bị gọi %d lần, mong %d", c.ten, i, b.goi, c.daGoi[i])
			}
		}
	}
}

func TestChuoiKhongBiet_GiuLoiGocDauTien(t *testing.T) {
	a, b := &LoiHaTang{ChiTiet: "a"}, &LoiHaTang{ChiTiet: "b"}
	kq := GiaiQuyet(context.Background(), &http.Request{}, []Bo{&boGia{err: a}, &boGia{err: b}}, nil)
	if kq.Loai != KhongBiet || kq.Loi != error(a) {
		t.Fatalf("giữ lỗi đầu tiên: %+v", kq)
	}
}

func TestChuoiBaoLoiNilKhongPanic(t *testing.T) {
	kq := GiaiQuyet(context.Background(), &http.Request{}, []Bo{&boGia{err: ErrTokenSai}, &boGia{ma: ".."}}, nil)
	if kq.Loai != ChuaDangNhap {
		t.Fatalf("%+v", kq)
	}
}
