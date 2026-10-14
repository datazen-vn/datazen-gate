package quyetdinh

import (
	"errors"
	"fmt"
	"strings"

	"github.com/datazen-vn/market-gate/internal/hub"
)

// KiemPaywall liệt kê MỌI lỗi cấu hình (rỗng = hợp lệ). Không ném ở lần đầu gặp lỗi để `--check` báo đủ.
func KiemPaywall(c CauHinhPaywall) []string {
	var ds []string
	if !maGoiRe.MatchString(c.MaGoi) {
		ds = append(ds, "paywall.plan_code phải khớp ^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$ (chữ thường, số, gạch ngang; ≤ 64)")
	}
	if n := len(c.PublicOrigins); n < 1 || n > 8 {
		ds = append(ds, "paywall.public_origins phải là danh sách 1–8 origin của chính app")
	}
	for i, o := range c.PublicOrigins {
		if _, err := phanTichOrigin(o); err != nil {
			ds = append(ds, fmt.Sprintf("paywall.public_origins[%d]: %v", i, err))
		}
	}
	for ten, p := range map[string]string{"success_path": c.SuccessPath, "cancel_path": c.CancelPath} {
		if err := kiemDuongDan(p); err != nil {
			ds = append(ds, "paywall."+ten+": "+err.Error())
		}
	}
	if c.LoginURL != "" {
		if !duongDanHopLe(c.LoginURL) {
			if _, err := hub.PhanTichURLAnToan(c.LoginURL, "paywall.login_url"); err != nil || !inDuoc(c.LoginURL) {
				ds = append(ds, `paywall.login_url phải là đường dẫn "/login" hoặc URL tuyệt đối, ASCII in được`)
			}
		}
	}
	return ds
}

func phanTichOrigin(raw string) (origin, error) {
	u, err := hub.PhanTichURLAnToan(raw, "origin")
	if err != nil {
		return origin{}, err
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(raw, "?#") {
		return origin{}, errors.New("chỉ gồm origin (https://host[:port]), không có đường dẫn/query")
	}
	return origin{goc: u.Scheme + "://" + u.Host, host: strings.ToLower(u.Host)}, nil
}

// duongDanHopLe: bắt đầu bằng "/" (không "//" hay "/\"), chỉ ASCII in được.
func duongDanHopLe(s string) bool {
	if len(s) < 1 || s[0] != '/' || (len(s) > 1 && (s[1] == '/' || s[1] == '\\')) {
		return false
	}
	return inDuoc(s)
}

func inDuoc(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return s != ""
}

func kiemDuongDan(p string) error {
	if p == "" {
		return nil
	}
	if !duongDanHopLe(p) {
		return errors.New(`phải là đường dẫn bắt đầu bằng "/" (không "//"), ASCII in được`)
	}
	// Query/fragment làm success_url/cancel_url lệch khỏi đường dẫn thật và bypass so chuỗi thô sẽ không bao giờ khớp.
	if strings.ContainsAny(p, "?#") {
		return errors.New(`chỉ là đường dẫn, không có "?" hoặc "#"`)
	}
	return nil
}
