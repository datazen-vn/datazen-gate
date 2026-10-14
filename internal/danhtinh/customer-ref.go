// Package danhtinh nhận diện khách cuối của app dev: chuỗi resolver (JWT, session hook) giải ra `customer_ref`.
//
// customer_ref do CHÍNH app của dev đặt (không phải tài khoản DataZen). Hub không chuẩn hoá: phân biệt hoa/thường, duy nhất theo
// (app, môi trường, ref). Không bao giờ đưa giá trị ref vào thông báo lỗi/log — dev hay dùng email làm ref nên đó có thể là dữ liệu
// cá nhân.
package danhtinh

// MaKhachHopLe: 1–200 ký tự ASCII in được, không khoảng trắng, trừ "." và ".." (ref nằm trong đường dẫn `/v1/customers/{ref}/…`:
// URL chuẩn hoá chúng — cả dạng `%2e` — thành đoạn đường dẫn khác).
func MaKhachHopLe(s string) bool {
	if len(s) < 1 || len(s) > 200 || s == "." || s == ".." {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}
