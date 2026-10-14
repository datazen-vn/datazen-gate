package tuyen

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

const soVongGiaiMa = 3

// DangDuongDan trả bốn dạng của một đường dẫn THÔ (không query, chưa chuẩn hoá — giữ nguyên `/api/dz/../premium/x`), bỏ trùng:
// thô; giải mã (phần trăm LẶP tối đa 3 vòng, dừng sớm khi một vòng không đổi); chuẩn hoá từ dạng giải mã (`\`→`/`, cắt `;tham-số`
// ở MỖI đoạn, gộp `//`, giải `.`/`..` không lên quá gốc); chữ thường (dạng chuẩn hoá hạ chữ thường ASCII — Express/Fastify định
// tuyến không phân biệt hoa/thường).
func DangDuongDan(tho string) []string {
	giaiMa := tho
	for vong := 0; vong < soVongGiaiMa; vong++ {
		tiep := giaiPhanTram(giaiMa)
		if tiep == giaiMa {
			break
		}
		giaiMa = tiep
	}
	chuanHoa := chuanHoaDuongDan(giaiMa)
	var dang []string
	for _, d := range []string{tho, giaiMa, chuanHoa, haChuThuongASCII(chuanHoa)} {
		if !chua(dang, d) {
			dang = append(dang, d)
		}
	}
	return dang
}

func chua(ds []string, s string) bool {
	for _, d := range ds {
		if d == s {
			return true
		}
	}
	return false
}

// giaiPhanTram giải `%XX`; chuỗi hỏng (`%zz`, `%` cụt, UTF-8 sai) thì chỉ giải phần ASCII (`%00`–`%7F`) — đủ cho mẫu glob ASCII,
// không bỏ cuộc giữa chừng (khớp SDK: decodeURIComponent ném ⇒ giải riêng phần ASCII).
func giaiPhanTram(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	if r, err := url.PathUnescape(s); err == nil && utf8.ValidString(r) {
		return r
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			hi, ok1 := hex(s[i+1])
			lo, ok2 := hex(s[i+2])
			if ok1 && ok2 && hi < 8 {
				b.WriteByte(hi<<4 | lo)
				i += 2
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func hex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

func chuanHoaDuongDan(p string) string {
	var ra []string
	for _, doan := range strings.Split(strings.ReplaceAll(p, "\\", "/"), "/") {
		if i := strings.IndexByte(doan, ';'); i >= 0 {
			doan = doan[:i]
		}
		switch doan {
		case "", ".":
		case "..":
			if len(ra) > 0 {
				ra = ra[:len(ra)-1]
			}
		default:
			ra = append(ra, doan)
		}
	}
	return "/" + strings.Join(ra, "/")
}

// haChuThuongASCII hạ chữ thường CHỈ ASCII (strings.ToLower còn đổi chữ Unicode, lệch với SDK).
func haChuThuongASCII(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}
