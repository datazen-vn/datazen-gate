package hub

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

var loopbackCoCongRe = regexp.MustCompile(`^(?i)http://[^/?#@]+:[0-9]{1,5}([/?#]|$)`)

var maLoopback = map[string]bool{"localhost": true, "127.0.0.1": true}

// PhanTichURLAnToan: chỉ https; http chỉ cho `localhost`/`127.0.0.1` CÓ CỔNG viết rõ (bí mật không đi rõ qua mạng; cùng luật
// loopback với `success_url` của hub — `http://localhost` trơn và `[::1]` bị từ chối); không user:password.
func PhanTichURLAnToan(raw, nhan string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("%s rỗng", nhan)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("%s không phải URL hợp lệ", nhan)
	}
	loopback := maLoopback[u.Hostname()] && loopbackCoCongRe.MatchString(raw)
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return nil, fmt.Errorf("%s phải là https (http chỉ chấp nhận cho localhost:<cổng> / 127.0.0.1:<cổng>) — bí mật không được đi rõ qua mạng", nhan)
	}
	if u.User != nil {
		return nil, fmt.Errorf("%s không được chứa user:password", nhan)
	}
	return u, nil
}
