package danhtinh

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/datazen-vn/market-gate/internal/gopgoi"
)

const (
	toiDaThanJWKS    = 256 << 10
	tuoiToiDaJWKS    = 10 * time.Minute
	macDinhCooldown  = 30 * time.Second
	macDinhTimeoutTr = 5 * time.Second
)

// LoiHaTang: không hỏi được nguồn danh tính (JWKS không tải được, session hook lỗi…). KHÔNG bao giờ đồng nghĩa với "chưa đăng nhập".
type LoiHaTang struct {
	ChiTiet string
	Goc     error
}

func (l *LoiHaTang) Error() string { return "dz_identity_unavailable: " + l.ChiTiet }
func (l *LoiHaTang) Unwrap() error { return l.Goc }

type khoaJWK struct {
	kid, alg, loai string // loai: "RSA" | "EC:P-256" | "EC:P-384" | "EC:P-521" | "OKP"
	pub            any
}

// JWKS tải và đệm tập khoá công khai của app dev. Tải lại khi: chưa có khoá, quá 10 phút, hoặc `kid` lạ (tối đa 1 lần / cooldown —
// chống kẻ tấn công gửi kid bậy để đập máy chủ JWKS). Tải lỗi mà còn khoá cũ ⇒ vẫn dùng khoá cũ nếu tìm được; không thì LoiHaTang.
type JWKS struct {
	url      string
	http     *http.Client
	timeout  time.Duration
	cooldown time.Duration
	now      func() time.Time

	mu      sync.Mutex
	khoa    []khoaJWK
	taiLuc  time.Time
	daTai   bool
	nhomTai gopgoi.Nhom[struct{}]
}

// MoiJWKS: `client` nil ⇒ client mặc định không theo chuyển hướng. `url` phải đã được kiểm (https/loopback) ở cấu hình.
func MoiJWKS(url string, client *http.Client, timeout, cooldown time.Duration, now func() time.Time) *JWKS {
	if timeout <= 0 {
		timeout = macDinhTimeoutTr
	}
	if cooldown < 0 {
		cooldown = macDinhCooldown
	}
	if now == nil {
		now = time.Now
	}
	return &JWKS{url: url, http: khongTheoChuyenHuong(client), timeout: timeout, cooldown: cooldown, now: now}
}

func khongTheoChuyenHuong(goc *http.Client) *http.Client {
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if goc != nil {
		c.Transport = goc.Transport
	}
	return c
}

// Khoa trả khoá công khai ứng với (kid, alg). kid rỗng ⇒ chỉ nhận khi có ĐÚNG MỘT khoá hợp thuật toán. Không có ⇒ ErrTokenSai;
// không tải được JWKS ⇒ *LoiHaTang (không bao giờ chấp nhận token khi chưa kiểm được chữ ký).
func (j *JWKS) Khoa(ctx context.Context, kid, alg string) (any, error) {
	if k, ok := j.tim(kid, alg); ok && !j.canTaiLai(false) {
		return k, nil
	}
	if j.canTaiLai(true) {
		if err := j.tai(ctx); err != nil {
			if k, ok := j.tim(kid, alg); ok {
				return k, nil
			}
			return nil, err
		}
	}
	if k, ok := j.tim(kid, alg); ok {
		return k, nil
	}
	return nil, fmt.Errorf("%w: không có khoá JWKS khớp kid/alg", ErrTokenSai)
}

// canTaiLai: kidLa = true khi khoá cần tìm CHƯA có (kid lạ) ⇒ chịu cooldown; false ⇒ chỉ tải lại khi dữ liệu quá cũ.
func (j *JWKS) canTaiLai(kidLa bool) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if !j.daTai {
		return true
	}
	tuoi := j.now().Sub(j.taiLuc)
	if tuoi >= tuoiToiDaJWKS {
		return true
	}
	return kidLa && tuoi >= j.cooldown
}

func (j *JWKS) tim(kid, alg string) (any, bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	var khop []khoaJWK
	for _, k := range j.khoa {
		if !hopThuatToan(k, alg) || (k.alg != "" && k.alg != alg) {
			continue
		}
		if kid != "" && k.kid != kid {
			continue
		}
		khop = append(khop, k)
	}
	if len(khop) == 1 || (kid != "" && len(khop) > 0) {
		return khop[0].pub, true
	}
	return nil, false
}

func hopThuatToan(k khoaJWK, alg string) bool {
	switch {
	case strings.HasPrefix(alg, "RS"), strings.HasPrefix(alg, "PS"):
		return k.loai == "RSA"
	case alg == "ES256":
		return k.loai == "EC:P-256"
	case alg == "ES384":
		return k.loai == "EC:P-384"
	case alg == "ES512":
		return k.loai == "EC:P-521"
	case alg == "EdDSA":
		return k.loai == "OKP"
	}
	return false
}

func (j *JWKS) tai(ctx context.Context) error {
	_, err, _ := j.nhomTai.Chay("jwks", func() (struct{}, error) {
		khoa, err := j.keVe(ctx)
		if err != nil {
			return struct{}{}, err
		}
		j.mu.Lock()
		j.khoa, j.taiLuc, j.daTai = khoa, j.now(), true
		j.mu.Unlock()
		return struct{}{}, nil
	})
	return err
}

func (j *JWKS) keVe(ctx context.Context) ([]khoaJWK, error) {
	ctx, huy := context.WithTimeout(context.WithoutCancel(ctx), j.timeout)
	defer huy()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, j.url, nil)
	if err != nil {
		return nil, &LoiHaTang{ChiTiet: "jwks_url không hợp lệ", Goc: err}
	}
	req.Header.Set("Accept", "application/json")
	res, err := j.http.Do(req)
	if err != nil {
		return nil, &LoiHaTang{ChiTiet: "không tải được JWKS", Goc: err}
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, &LoiHaTang{ChiTiet: fmt.Sprintf("JWKS trả HTTP %d (cần 200; không theo chuyển hướng)", res.StatusCode)}
	}
	than, err := io.ReadAll(io.LimitReader(res.Body, toiDaThanJWKS+1))
	if err != nil || len(than) > toiDaThanJWKS {
		return nil, &LoiHaTang{ChiTiet: "đọc thân JWKS thất bại hoặc quá 256 KB", Goc: err}
	}
	khoa, err := DocJWKS(than)
	if err != nil {
		return nil, &LoiHaTang{ChiTiet: err.Error(), Goc: err}
	}
	return khoa, nil
}
