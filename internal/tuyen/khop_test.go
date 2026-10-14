package tuyen

import (
	"reflect"
	"testing"
)

func TestKhopGlob(t *testing.T) {
	cacTruongHop := []struct {
		mau, chuoi string
		mong       bool
	}{
		{"/x/*", "/x", true}, // gốc của thư mục không lọt ra ngoài
		{"/x/*", "/x/", true},
		{"/x/*", "/x/y/z", true},
		{"/x/*", "/xylophone", false},
		{"/x", "/x/", false},
		{"/a*c", "/abbbc", true},
		{"/a*c", "/abbbd", false},
		{"*", "", true},
		{"/**/x", "/a/b/x", true},
		{"/Premium/*", "/premium/x", false}, // phân biệt hoa/thường
		{"/a*a*a*a*a*a*b", "/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaac", false},
	}
	for _, c := range cacTruongHop {
		if got := KhopGlob(c.mau, c.chuoi); got != c.mong {
			t.Errorf("KhopGlob(%q, %q) = %v, mong %v", c.mau, c.chuoi, got, c.mong)
		}
	}
}

func TestKhopGlobKhongBungNoBacktracking(t *testing.T) {
	// Nhiều dấu `*` + chuỗi dài không khớp phải xong tức thì (O(n*m)); nếu cài bằng regexp backtracking test sẽ treo.
	mau := "/a*a*a*a*a*a*a*a*a*a*a*a*a*b"
	chuoi := "/" + repeat('a', 5000)
	if KhopGlob(mau, chuoi) {
		t.Fatal("không được khớp")
	}
}

func repeat(c byte, n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = c
	}
	return string(b)
}

func TestDangDuongDan(t *testing.T) {
	cacTruongHop := []struct {
		ten, tho string
		mong     []string
	}{
		{"không đổi ⇒ một dạng", "/premium/x", []string{"/premium/x"}},
		{"hai vòng giải mã", "/%2570remium/x", []string{"/%2570remium/x", "/premium/x"}},
		{"ba vòng rồi dừng (còn % thì dùng vòng 3)", "/%25252570", []string{"/%25252570", "/%70"}},
		{"backslash", `/public/..\premium/x`, []string{`/public/..\premium/x`, "/premium/x"}},
		{"tham số đoạn", "/a;jsessionid=1/b", []string{"/a;jsessionid=1/b", "/a/b"}},
		{"gộp // và dấu chấm", "//premium/./x", []string{"//premium/./x", "/premium/x"}},
		{"không lên quá gốc", "/../../x", []string{"/../../x", "/x"}},
		{"hoa ⇒ thêm dạng chữ thường", "/PREMIUM/x", []string{"/PREMIUM/x", "/premium/x"}},
		{"%2F thành dấu gạch", "/x%2Fsecret", []string{"/x%2Fsecret", "/x/secret"}},
		{"%zz hỏng giữ nguyên", "/a%zzb", []string{"/a%zzb"}},
		{"UTF-8 sai: chỉ giải ASCII", "/%41%e9", []string{"/%41%e9", "/A%e9", "/a%e9"}},
		{"%5C sinh ra backslash rồi thành /", "/a%5Cb", []string{"/a%5Cb", `/a\b`, "/a/b"}},
	}
	for _, c := range cacTruongHop {
		if got := DangDuongDan(c.tho); !reflect.DeepEqual(got, c.mong) {
			t.Errorf("%s: DangDuongDan(%q) = %q, mong %q", c.ten, c.tho, got, c.mong)
		}
	}
}

func bangThu(macDinh string) *Bang {
	return TaoBang(
		append(append([]string{}, BypassMacDinh...), "/login", "/billing/return"),
		[]Dong{
			{Khop: "/public/*", HanhDong: Cho},
			{Khop: "/premium/*", HanhDong: Can, TinhNang: "premium"},
			{Khop: "/api/premium/*", HanhDong: Can, TinhNang: "export", CheDoLoi: "open"},
			{Khop: "/pricing", HanhDong: Mem},
		},
		macDinh,
	)
}

func TestXetTuyen(t *testing.T) {
	cacTruongHop := []struct {
		ten, tho string
		macDinh  string
		qua      bool
		tinhNang string // "" = không dòng; "-" = dòng không feature
		hanhDong string
	}{
		{"allow khai rõ", "/public/a", Cho, true, "", ""},
		{"require", "/premium/x", Cho, false, "premium", Can},
		{"soft", "/pricing", Cho, false, "-", Mem},
		{"bypass webhook", "/api/dz/webhook", Can, true, "", ""},
		{"bypass success_path", "/billing/return", Can, true, "", ""},
		{"không khớp + mặc định allow", "/linh-tinh", Cho, true, "", ""},
		{"không khớp + mặc định require", "/linh-tinh", Can, false, "", Can},
		{"hoa không lách require", "/PREMIUM/x", Cho, false, "premium", Can},
		{"hoa KHÔNG mở bởi allow (require mặc định)", "/PUBLIC/x", Can, false, "", Can},
		{"%70 giải mã", "/%70remium/x", Cho, false, "premium", Can},
		{"hai vòng giải mã", "/%2570remium/x", Cho, false, "premium", Can},
		{"// ở đầu", "//premium/x", Cho, false, "premium", Can},
		{"tham số đoạn", "/premium;x", Cho, false, "premium", Can},
		{"backslash", `/public/..\premium/x`, Cho, false, "premium", Can},
		{"/premium/../public/x thô khớp require", "/premium/../public/x", Cho, false, "premium", Can},
		{"bypass rồi .. ra require", "/api/dz/../../premium/x", Cho, false, "premium", Can},
		{"x%2Fsecret không lọt allow", "/public/..%2Fx/secret", Can, false, "", Can},
		{"không cắt query ở đây (đã bỏ)", "/premium/x", Cho, false, "premium", Can},
	}
	for _, c := range cacTruongHop {
		kq := bangThu(c.macDinh).Xet(c.tho)
		if kq.Qua != c.qua {
			t.Errorf("%s: Qua = %v, mong %v", c.ten, kq.Qua, c.qua)
			continue
		}
		if c.qua {
			continue
		}
		switch {
		case c.tinhNang == "" && c.hanhDong == Can:
			if kq.Dong != nil {
				t.Errorf("%s: mong tuyến mặc định (nil), có %+v", c.ten, kq.Dong)
			}
		default:
			if kq.Dong == nil {
				t.Errorf("%s: thiếu dòng", c.ten)
				continue
			}
			want := c.tinhNang
			if want == "-" {
				want = ""
			}
			if kq.Dong.HanhDong != c.hanhDong || kq.Dong.TinhNang != want {
				t.Errorf("%s: dòng %+v, mong %s/%s", c.ten, *kq.Dong, c.hanhDong, want)
			}
		}
	}
}

func TestNhieuDongRequireDongDungTruocThang(t *testing.T) {
	b := TaoBang(nil, []Dong{
		{Khop: "/a/*", HanhDong: Can, TinhNang: "truoc"},
		{Khop: "/b/*", HanhDong: Can, TinhNang: "sau"},
	}, Cho)
	// Thô khớp `/b/*` (dòng sau), chuẩn hoá `/a/x` khớp `/a/*` (dòng trước): dòng đứng TRƯỚC trong bảng thắng.
	kq := b.Xet("/b/../a/x")
	if kq.Dong == nil || kq.Dong.TinhNang != "truoc" {
		t.Fatalf("mong dòng 'truoc', có %+v", kq.Dong)
	}
}

func TestDuongDanTho(t *testing.T) {
	cacTruongHop := map[string]string{
		"/premium/x?a=1":                  "/premium/x",
		"/premium/../public/x#f":          "/premium/../public/x",
		"http://app:3000/api/dz/../x?q=1": "/api/dz/../x",
		"https://app.vn":                  "",
		"":                                "/",
		"*":                               "*",
	}
	for vao, mong := range cacTruongHop {
		if mong == "" {
			continue // `scheme://host` không có path: chỉ cần không panic
		}
		if got := DuongDanTho(vao); got != mong {
			t.Errorf("DuongDanTho(%q) = %q, mong %q", vao, got, mong)
		}
	}
	_ = DuongDanTho("https://app.vn")
}
