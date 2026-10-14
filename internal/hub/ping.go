package hub

import "context"

// MaKhachKiemTra là khách giả mà Ping hỏi (không phải khách thật; hub trả `none`).
const MaKhachKiemTra = "cus_test_doctor"

// Ping kiểm hub tới được và khoá API đúng: GET entitlements của khách giả, BỎ QUA bản đệm (Huy trước). Lỗi trả nguyên *Loi
// (khoá sai ⇒ dz_service_key_invalid, hub chết ⇒ dz_server_error/dz_network_error…) — không bao giờ coi lỗi là "sẵn sàng".
func (k *Khach) Ping(ctx context.Context) error {
	k.Huy(MaKhachKiemTra)
	_, err := k.Lay(ctx, MaKhachKiemTra, YeuCau{CheDoLoi: "closed"})
	return err
}
