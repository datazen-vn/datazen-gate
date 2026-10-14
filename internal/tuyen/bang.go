package tuyen

// BypassMacDinh luôn có: webhook/route nội bộ của SDK (`/api/dz/*`) và cổng vận hành của dz-gate (`/_dz/*`).
var BypassMacDinh = []string{"/api/dz/*", "/_dz/*"}

// Bang gom bypass + `routes` + hành động mặc định thành MỘT bảng first-match: bypass là các dòng `allow` đứng đầu, nên một dạng
// khớp bypass thì kết quả của dạng đó là bypass dù một dòng `routes` (kể cả `require` phủ `/*`) cũng khớp — và bypass chỉ thắng
// khi MỌI dạng cùng ra bypass (xem GiaiTuyen).
type Bang struct {
	dong    []Dong
	macDinh string // Cho | Can
}

// TaoBang: `bypass` là toàn bộ mẫu bypass (đã gồm BypassMacDinh, login_url, success_path…), `routes` là bảng của dev, `macDinh` là
// `allow` hoặc `require` (rỗng = allow).
func TaoBang(bypass []string, routes []Dong, macDinh string) *Bang {
	b := &Bang{macDinh: macDinh}
	if b.macDinh == "" {
		b.macDinh = Cho
	}
	for _, m := range bypass {
		b.dong = append(b.dong, Dong{Khop: m, HanhDong: Cho})
	}
	b.dong = append(b.dong, routes...)
	return b
}

// KetQua của việc xét một đường dẫn.
type KetQua struct {
	// Qua = true: cho qua không cần hỏi hub, không bắt buộc danh tính (bypass, tuyến allow, hoặc mặc định allow).
	Qua bool
	// Dong là dòng chi phối khi Qua = false (Can hoặc Mem); nil = tuyến mặc định `require` không feature.
	Dong *Dong
}

// Xet áp quy tắc cho đường dẫn THÔ (chưa chuẩn hoá, không query).
func (b *Bang) Xet(duongDanTho string) KetQua {
	d := GiaiTuyen(b.dong, DangDuongDan(duongDanTho))
	if (d != nil && d.HanhDong == Cho) || (d == nil && b.macDinh == Cho) {
		return KetQua{Qua: true}
	}
	return KetQua{Dong: d}
}

// DuongDanTho lấy đường dẫn thô (không query/fragment) từ RequestURI của yêu cầu — KHÔNG qua url.Parse chuẩn hoá. Dạng
// `scheme://host/path` (proxy) cũng được đọc; không đọc được thì trả "/".
func DuongDanTho(requestURI string) string {
	s := requestURI
	if i := indexAny(s, "?#"); i >= 0 {
		s = s[:i]
	}
	if i := indexScheme(s); i >= 0 {
		s = s[i:]
	}
	if s == "" {
		return "/"
	}
	return s
}

func indexAny(s, chars string) int {
	for i := 0; i < len(s); i++ {
		for j := 0; j < len(chars); j++ {
			if s[i] == chars[j] {
				return i
			}
		}
	}
	return -1
}

// indexScheme: với `scheme://authority/path` trả vị trí bắt đầu của `/path` (hoặc len(s) nếu không có path); không phải dạng đó ⇒ -1.
func indexScheme(s string) int {
	i := 0
	for i < len(s) && (isAlpha(s[i]) || (i > 0 && (isDigit(s[i]) || s[i] == '+' || s[i] == '-' || s[i] == '.'))) {
		i++
	}
	if i == 0 || i+2 >= len(s) || s[i] != ':' || s[i+1] != '/' || s[i+2] != '/' {
		return -1
	}
	for j := i + 3; j < len(s); j++ {
		if s[j] == '/' {
			return j
		}
	}
	return len(s)
}

func isAlpha(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }
