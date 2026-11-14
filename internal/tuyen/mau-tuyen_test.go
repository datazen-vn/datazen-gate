package tuyen

import "testing"

// Bang.Xet gắn nhãn tuyến hữu hạn cho log: glob của dòng routes, "bypass", hoặc "unmatched" — không bao giờ đường dẫn thật.
func TestXetGanMauTuyen(t *testing.T) {
	b := TaoBang(BypassMacDinh, []Dong{{Khop: "/dashboard/*", HanhDong: Can}, {Khop: "/pricing", HanhDong: Mem}, {Khop: "/api/public/*", HanhDong: Cho}}, "")
	for duong, mong := range map[string]string{
		"/dashboard/u@x.vn": "/dashboard/*",
		"/pricing":          "/pricing",
		"/api/public/a":     "/api/public/*",
		"/api/dz/hook":      "bypass",
		"/_dz/healthz":      "bypass",
		"/don-hang/777":     "unmatched",
	} {
		if got := b.Xet(duong).Mau; got != mong {
			t.Errorf("%s: Mau = %q, muốn %q", duong, got, mong)
		}
	}
}
