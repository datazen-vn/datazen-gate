package cauhinh

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// khoaTepToiDa: khoá API chỉ vài chục byte; tệp lớn hơn là nhầm đường dẫn (vd trỏ vào tệp dữ liệu) nên từ chối thay vì đọc hết.
const khoaTepToiDa = 4 << 10

// KhoaAPI trả khoá API của hub: biến `<api_key_env>` HOẶC nội dung tệp mà `<api_key_env>_FILE` trỏ tới (Docker/Kubernetes secrets,
// không hiện ra ở `docker inspect`). Đặt cả hai là lỗi (không đoán cái nào đúng). Tệp phải là tệp thường ≤ 4 KiB, không rỗng; xuống dòng/khoảng
// trắng ở hai đầu bị cắt. Quyền rộng hơn 0600 (nhóm/người khác đọc được) KHÔNG phải lỗi (secret của Docker mặc định 0444, của Kubernetes 0644)
// mà là cảnh báo. Lỗi và cảnh báo không bao giờ chứa nội dung khoá. `co` = false khi không có nguồn nào (Kiem báo "chưa đặt").
func (c *Config) KhoaAPI(env Env) (khoa string, co bool, canh []string, err error) {
	ten := c.Hub.APIKeyEnv
	v, coBien := env(ten)
	coBien = coBien && strings.TrimSpace(v) != ""
	duong, coTep := env(ten + "_FILE")
	coTep = coTep && strings.TrimSpace(duong) != ""
	switch {
	case coBien && coTep:
		return "", false, nil, fmt.Errorf("đặt cả %s và %s_FILE: chỉ dùng MỘT nguồn", ten, ten)
	case coBien:
		return v, true, nil, nil
	case !coTep:
		return "", false, nil, nil
	}
	khoa, canh, err = docKhoaTep(ten+"_FILE", duong)
	return khoa, err == nil, canh, err
}

func docKhoaTep(tenBien, duong string) (string, []string, error) {
	f, err := os.Open(duong)
	if err != nil {
		return "", nil, fmt.Errorf("không mở được tệp khoá API (%s): %w", tenBien, errors.Unwrap(err))
	}
	defer f.Close()
	st, err := f.Stat()
	switch {
	case err != nil:
		return "", nil, fmt.Errorf("không đọc được thuộc tính tệp khoá API (%s): %w", tenBien, err)
	case !st.Mode().IsRegular():
		return "", nil, fmt.Errorf("%s=%q không phải tệp thường", tenBien, duong)
	}
	b, err := io.ReadAll(io.LimitReader(f, khoaTepToiDa+1))
	if err != nil {
		return "", nil, fmt.Errorf("không đọc được tệp khoá API (%s): %w", tenBien, err)
	}
	khoa := strings.TrimSpace(string(b))
	switch {
	case len(b) > khoaTepToiDa:
		return "", nil, fmt.Errorf("tệp khoá API (%s) lớn quá %d byte: nhầm đường dẫn?", tenBien, khoaTepToiDa)
	case khoa == "":
		return "", nil, fmt.Errorf("tệp khoá API (%s) rỗng", tenBien)
	}
	var canh []string
	if m := st.Mode().Perm(); m&0o077 != 0 {
		canh = append(canh, fmt.Sprintf("tệp khoá API (%s) có quyền %#o: nhóm/người khác đọc được; đặt 0400 hoặc 0600", tenBien, m))
	}
	return khoa, canh, nil
}
