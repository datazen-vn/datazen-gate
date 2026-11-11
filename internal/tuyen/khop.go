// Package tuyen khớp đường dẫn với bảng tuyến theo CHUẨN CHUNG của SDK và datazen-gate (README conformance gate-v1, mục "Khớp tuyến").
//
// An toàn: đường dẫn là dữ liệu kẻ tấn công điều khiển. Upstream có thể giải mã/chuẩn hoá khác ta (`/a%2Fb`, `//`, `\`, `;tham-số`,
// hoa/thường) nên mỗi đường dẫn được xét ở NHIỀU DẠNG (xem DangDuongDan). Nguyên tắc: dạng thêm vào chỉ CHẶN THÊM, không MỞ THÊM.
package tuyen

// Hanh dong của một dòng tuyến.
const (
	Cho = "allow"   // cho qua, không hỏi hub, không cần danh tính
	Can = "require" // cần danh tính + thuê bao còn hiệu lực
	Mem = "soft"    // không bao giờ chặn, nhưng vẫn tiêm quyền nếu có danh tính
)

// Dong là một dòng của bảng `routes`.
type Dong struct {
	Khop     string // glob, bắt đầu bằng "/"
	HanhDong string // Cho | Can | Mem
	TinhNang string // tuỳ chọn: khoá trong features
	MaGoi    string // tuỳ chọn: ghi đè paywall.plan_code cho dòng này
	CheDoLoi string // tuỳ chọn: "closed" | "open"
}

// KhopGlob: `*` khớp mọi chuỗi (kể cả rỗng và `/`), so NGUYÊN VĂN (phân biệt hoa/thường; hoa/thường do dạng chữ thường của
// DangDuongDan lo). Mẫu kết thúc `/*` còn khớp phần gốc (`/x/*` khớp `/x`) để gốc của thư mục không lọt ra ngoài.
// Thuật toán hai con trỏ O(n*m): không bùng nổ backtracking với nhiều dấu `*`.
func KhopGlob(mau, chuoi string) bool {
	if khopMotLan(mau, chuoi) {
		return true
	}
	return len(mau) >= 2 && mau[len(mau)-2:] == "/*" && khopMotLan(mau[:len(mau)-2], chuoi)
}

func khopMotLan(p, s string) bool {
	pi, si := 0, 0
	sao, danhDau := -1, 0
	for si < len(s) {
		switch {
		case pi < len(p) && p[pi] == '*':
			sao = pi
			pi++
			danhDau = si
		case pi < len(p) && p[pi] == s[si]:
			pi++
			si++
		case sao >= 0:
			pi = sao + 1
			danhDau++
			si = danhDau
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}

// GiaiTuyen áp quy tắc README: mỗi dạng tìm dòng ĐẦU TIÊN khớp rồi hợp lại:
//  1. MỘT dạng ra `require` ⇒ dòng `require` đó (nhiều dòng: dòng đứng trước trong bảng thắng);
//  2. không thì, `allow`/`soft` chỉ khi MỌI dạng đều khớp và cùng một hành động;
//  3. còn lại (không dạng nào khớp, hoặc các dạng bất đồng) ⇒ nil = theo hành động mặc định.
//
// Bypass đứng đầu bảng dưới dạng dòng `allow` nên thắng một tuyến `require` chỉ khi MỌI dạng đều bypass.
func GiaiTuyen(bang []Dong, dang []string) *Dong {
	trung := make([]int, len(dang))
	can := -1
	for i, d := range dang {
		trung[i] = -1
		for j := range bang {
			if KhopGlob(bang[j].Khop, d) {
				trung[i] = j
				break
			}
		}
		if trung[i] >= 0 && bang[trung[i]].HanhDong == Can && (can < 0 || trung[i] < can) {
			can = trung[i]
		}
	}
	if can >= 0 {
		return &bang[can]
	}
	if len(trung) == 0 || trung[0] < 0 {
		return nil
	}
	hanhDong := bang[trung[0]].HanhDong
	for _, j := range trung {
		if j < 0 || bang[j].HanhDong != hanhDong {
			return nil
		}
	}
	return &bang[trung[0]]
}
