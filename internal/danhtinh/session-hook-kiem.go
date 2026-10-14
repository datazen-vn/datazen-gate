package danhtinh

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var (
	hookHostRe   = regexp.MustCompile(`^(?:[a-z0-9.-]+|\[[0-9a-f:]+\])(?::[0-9]{1,5})?$`)
	hookHeaderRe = regexp.MustCompile(`^[a-z0-9-]{1,64}$`)
	hookDuongRe  = regexp.MustCompile(`^[A-Za-z0-9_]+(\.[A-Za-z0-9_]+){0,4}$`)
	hookLoopback = map[string]bool{"localhost": true, "127.0.0.1": true, "::1": true}
)

func kiemURLHook(raw string, allowHosts []string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, errors.New("session_hook.url không hợp lệ")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, errors.New("session_hook.url phải là http(s)")
	}
	if u.User != nil {
		return nil, errors.New("session_hook.url không được chứa user:password")
	}
	allow := map[string]bool{}
	for _, h := range allowHosts {
		h = strings.ToLower(strings.TrimSpace(h))
		if !hookHostRe.MatchString(h) {
			return nil, errors.New("session_hook.allow_hosts[] phải là tên host (tuỳ chọn :port), không ký tự đại diện")
		}
		allow[h] = true
	}
	host := strings.ToLower(u.Hostname())
	if !hookLoopback[host] && !allow[host] && !allow[strings.ToLower(u.Host)] {
		return nil, fmt.Errorf("session_hook.url trỏ tới host %q: chỉ loopback hoặc host trong allow_hosts mới được nhận cookie của khách (chống SSRF)", u.Host)
	}
	return u, nil
}

func kiemDuongDanChuThe(raw string) ([]string, error) {
	if !hookDuongRe.MatchString(raw) {
		return nil, errors.New("session_hook.subject_path phải dạng `user.id` (tối đa 5 đoạn [A-Za-z0-9_])")
	}
	doan := strings.Split(raw, ".")
	for _, d := range doan {
		if d == "__proto__" || d == "constructor" || d == "prototype" {
			return nil, errors.New("session_hook.subject_path không được chứa __proto__/constructor/prototype")
		}
	}
	return doan, nil
}

type capHeader struct{ ten, giaTri string }

// bam: SHA-256 của các cặp header (có độ dài tiền tố ⇒ không nhập nhằng), để không giữ cookie/token thô trong bộ nhớ.
func bam(cacHeader []capHeader) string {
	h := sha256.New()
	for _, c := range cacHeader {
		fmt.Fprintf(h, "%d:%s%d:%s", len(c.ten), c.ten, len(c.giaTri), c.giaTri)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// chuTheTuJSON đi theo `doan` chỉ qua thuộc tính của đối tượng JSON; không có/null/rỗng ⇒ "" (chưa đăng nhập); chuỗi hoặc số
// nguyên an toàn ⇒ chuỗi; kiểu khác là lỗi cấu hình subject_path (không đoán).
func chuTheTuJSON(body any, doan []string) (string, error) {
	cur := body
	for _, d := range doan {
		m, ok := cur.(map[string]any)
		if !ok {
			return "", nil
		}
		if cur, ok = m[d]; !ok {
			return "", nil
		}
	}
	switch x := cur.(type) {
	case nil:
		return "", nil
	case string:
		return x, nil
	case json.Number:
		if n, err := strconv.ParseInt(x.String(), 10, 64); err == nil && n >= -(1<<53-1) && n <= 1<<53-1 {
			return strconv.FormatInt(n, 10), nil
		}
	}
	return "", &LoiHaTang{ChiTiet: "session hook trả giá trị không phải chuỗi/số nguyên (kiểm tra subject_path)"}
}
