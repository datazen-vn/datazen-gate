package danhtinh

import (
	"context"
	"errors"
	"net/http"
)

// Bo là một resolver danh tính (JWT, session hook…). Trả ("", nil) = KHÔNG nhận ra ai (chưa đăng nhập).
type Bo interface {
	NhanDien(ctx context.Context, r *http.Request) (string, error)
}

// ErrTokenSai: token sai chữ ký/hết hạn/thiếu claim ⇒ coi như resolver này không nhận ra ai (thử resolver kế). Khác hẳn với
// "không hỏi được" (JWKS không tải được, session hook timeout…): đó KHÔNG phải là "chưa đăng nhập".
var ErrTokenSai = errors.New("dz_token_invalid")

// Loai kết quả của chuỗi.
type Loai int

const (
	// ChuaDangNhap: không resolver nào nhận ra khách ⇒ 401/302 login.
	ChuaDangNhap Loai = iota
	// CoKhach: MaKhach hợp lệ.
	CoKhach
	// KhongBiet: có resolver LỖI (timeout, 5xx, JWKS không tải được…) và không ai khác nhận ra khách ⇒ 503 hoặc cho qua theo
	// fail_mode — KHÔNG phải "chưa đăng nhập" (sẽ đẩy người đang đăng nhập sang trang login).
	KhongBiet
)

// Ket quả của chuỗi.
type Ket struct {
	Loai    Loai
	MaKhach string
	Loi     error // khi KhongBiet
}

// ErrMaKhachSai: danh tính giải ra customer_ref sai hình dạng (kể cả "."/"..").
var ErrMaKhachSai = errors.New("dz_invalid_customer_ref")

// GiaiQuyet chạy lần lượt các resolver. Quy tắc:
//   - resolver đầu trả khách hợp lệ ⇒ dừng; "" ⇒ thử resolver kế;
//   - ErrTokenSai ⇒ coi như resolver đó không nhận ra ai (+ baoLoi), thử tiếp;
//   - lỗi khác ⇒ KHÔNG biết là ai: ghi nhớ rồi thử tiếp; hết chuỗi mà không ai nhận ra khách ⇒ KhongBiet;
//   - customer_ref sai hình dạng ⇒ chưa đăng nhập (+ baoLoi); DỪNG chuỗi, không để resolver sau ghi đè.
//
// `baoLoi` (có thể nil) nhận mọi lỗi để log/metric; lỗi KHÔNG chứa giá trị cookie/token/ref.
func GiaiQuyet(ctx context.Context, r *http.Request, bo []Bo, baoLoi func(error)) Ket {
	bao := func(err error) {
		if baoLoi != nil {
			baoLoi(err)
		}
	}
	var khongBiet error
	for _, b := range bo {
		ma, err := b.NhanDien(ctx, r)
		if err != nil {
			bao(err)
			if errors.Is(err, ErrTokenSai) {
				continue
			}
			if khongBiet == nil {
				khongBiet = err
			}
			continue
		}
		if ma == "" {
			continue
		}
		if MaKhachHopLe(ma) {
			return Ket{Loai: CoKhach, MaKhach: ma}
		}
		bao(ErrMaKhachSai)
		return Ket{Loai: ChuaDangNhap}
	}
	if khongBiet != nil {
		return Ket{Loai: KhongBiet, Loi: khongBiet}
	}
	return Ket{Loai: ChuaDangNhap}
}
