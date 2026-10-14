package cauhinh

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/datazen-vn/market-gate/internal/hub"
	"github.com/datazen-vn/market-gate/internal/quyetdinh"
)

// Muc của một vấn đề.
const (
	MucLoi     = "lỗi"
	MucCanhBao = "cảnh báo"
)

const (
	soDanhTinhToiDa = 8
	soTuyenToiDa    = 128
	soStripToiDa    = 16
	ttlToiDa        = hub.TTLToiDa
	staleToiDa      = hub.StaleToiDa
	timeoutToiDa    = 60 * time.Second
)

var (
	khoaEnvRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	matchRe   = regexp.MustCompile(`^/[\x21-\x7e]{0,200}$`)
	featureRe = regexp.MustCompile(`^[^\x00-\x20\x7f,]{1,128}$`)
	stripRe   = regexp.MustCompile(`^[a-z0-9*-]{1,64}$`)
	maGoiRe   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$`)
)

// VanDe là một lỗi/cảnh báo của cấu hình. Truong là đường dẫn trường (`hub.cache_ttl`, `routes[2].match`).
type VanDe struct {
	Muc, Truong, NoiDung string
}

func (v VanDe) String() string { return fmt.Sprintf("[%s] %s: %s", v.Muc, v.Truong, v.NoiDung) }

// KetQuaKiem gom mọi vấn đề.
type KetQuaKiem struct{ Cac []VanDe }

func (k *KetQuaKiem) loi(truong, f string, a ...any) {
	k.Cac = append(k.Cac, VanDe{MucLoi, truong, fmt.Sprintf(f, a...)})
}
func (k *KetQuaKiem) canh(truong, f string, a ...any) {
	k.Cac = append(k.Cac, VanDe{MucCanhBao, truong, fmt.Sprintf(f, a...)})
}

// HopLe: không có lỗi (cảnh báo không làm cấu hình hỏng).
func (k *KetQuaKiem) HopLe() bool {
	for _, v := range k.Cac {
		if v.Muc == MucLoi {
			return false
		}
	}
	return true
}

// Env tra biến môi trường (os.LookupEnv, hoặc bản giả khi test).
type Env func(ten string) (string, bool)

// Kiem kiểm MỌI luật và liệt kê hết (không dừng ở lỗi đầu). `c.MacDinh()` phải được gọi trước. Luật (kế hoạch §1.3 + SDK):
// listen ≠ upstream; hub https (http chỉ loopback có cổng); khoá env có + tiền tố dz_test_/dz_live_ (cảnh báo live + fail_mode open);
// ≥ 1 tuyến, glob hợp lệ; success_path không khớp tuyến `require` (cảnh báo); session_hook url loopback/allow_hosts;
// cache_ttl ≤ 10m; stale_if_error ≤ 1h; danh tính dựng được; paywall hợp lệ.
func Kiem(c *Config, env Env) *KetQuaKiem {
	k := &KetQuaKiem{}
	kiemGate(k, c)
	kiemHub(k, c, env)
	kiemDanhTinh(k, c, env)
	kiemTuyen(k, c)
	kiemPaywall(k, c)
	kiemInjectOps(k, c)
	return k
}

func kiemGate(k *KetQuaKiem, c *Config) {
	if c.Gate.Mode != "proxy" {
		k.loi("gate.mode", "chỉ hỗ trợ \"proxy\", nhận %q", c.Gate.Mode)
	}
	host, port, ok := tachDiaChi(c.Gate.Listen)
	if !ok {
		k.loi("gate.listen", "phải dạng host:port hoặc :port (vd \":8080\"), nhận %q", c.Gate.Listen)
	}
	u, err := url.Parse(c.Gate.Upstream)
	switch {
	case c.Gate.Upstream == "":
		k.loi("gate.upstream", "thiếu (địa chỉ app của bạn, vd http://app:3000)")
	case err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https"):
		k.loi("gate.upstream", "phải là URL http(s) có host, nhận %q", c.Gate.Upstream)
	case u.User != nil:
		k.loi("gate.upstream", "không được chứa user:password")
	case ok && gateTuTroVaoMinh(host, port, u):
		k.loi("gate.upstream", "trùng với gate.listen (%s): gate sẽ tự proxy chính nó", c.Gate.Listen)
	}
}

// gateTuTroVaoMinh: upstream cùng cổng với listen và host là loopback/địa chỉ trống ⇒ vòng lặp.
func gateTuTroVaoMinh(listenHost, listenPort string, up *url.URL) bool {
	upPort := up.Port()
	if upPort == "" {
		upPort = map[string]string{"http": "80", "https": "443"}[up.Scheme]
	}
	if upPort != listenPort {
		return false
	}
	local := func(h string) bool {
		h = strings.ToLower(h)
		return h == "" || h == "localhost" || h == "0.0.0.0" || h == "::" || h == "::1" || strings.HasPrefix(h, "127.")
	}
	return strings.EqualFold(up.Hostname(), listenHost) || (local(listenHost) && local(up.Hostname()))
}

func tachDiaChi(s string) (host, port string, ok bool) {
	h, p, err := net.SplitHostPort(s)
	if err != nil {
		return "", "", false
	}
	n, err := strconv.Atoi(p)
	return h, p, err == nil && n >= 1 && n <= 65535
}

func kiemHub(k *KetQuaKiem, c *Config, env Env) {
	h := c.Hub
	if _, err := hub.PhanTichURLAnToan(h.BaseURL, "hub.base_url"); err != nil {
		k.loi("hub.base_url", "%v", err)
	}
	if h.FailMode != "open" && h.FailMode != "closed" {
		k.loi("hub.fail_mode", "phải là open hoặc closed, nhận %q", h.FailMode)
	}
	ttl := time.Duration(0)
	if h.CacheTTL != nil {
		ttl = h.CacheTTL.D()
	}
	if ttl < 0 || ttl > ttlToiDa {
		k.loi("hub.cache_ttl", "phải trong [0s, %s] (thu hồi quyền không được trễ quá 10 phút)", ttlToiDa)
	}
	if st := h.StaleIfError.D(); st < 0 || st > staleToiDa {
		k.loi("hub.stale_if_error", "phải trong [0s, %s]", staleToiDa)
	} else if st > 0 && ttl == 0 {
		k.loi("hub.stale_if_error", "> 0 cần bộ đệm: đặt hub.cache_ttl > 0")
	}
	if t := h.Timeout.D(); t <= 0 || t > timeoutToiDa {
		k.loi("hub.timeout", "phải trong (0s, %s]", timeoutToiDa)
	}
	if !khoaEnvRe.MatchString(h.APIKeyEnv) {
		k.loi("hub.api_key_env", "phải là tên biến môi trường hợp lệ, nhận %q", h.APIKeyEnv)
		return
	}
	khoa, co := env(h.APIKeyEnv)
	switch {
	case !co || strings.TrimSpace(khoa) == "":
		k.loi("hub.api_key_env", "biến môi trường %s chưa đặt (khoá API dz_test_… / dz_live_…)", h.APIKeyEnv)
	case !strings.HasPrefix(khoa, "dz_test_") && !strings.HasPrefix(khoa, "dz_live_"):
		k.loi("hub.api_key_env", "khoá trong %s phải bắt đầu bằng dz_test_ hoặc dz_live_", h.APIKeyEnv)
	case len(khoa) <= len("dz_test_"):
		k.loi("hub.api_key_env", "khoá trong %s quá ngắn", h.APIKeyEnv)
	case strings.HasPrefix(khoa, "dz_live_") && coFailMoCua(c):
		k.canh("hub.fail_mode", "khoá LIVE + fail_mode open: khi hub không trả lời, tuyến trả phí mở cửa miễn phí — chỉ nên bật cho tính năng không đụng tiền")
	}
}

func kiemPaywall(k *KetQuaKiem, c *Config) {
	for _, v := range quyetdinh.KiemPaywall(c.PaywallQuyet()) {
		k.loi("paywall", "%s", v)
	}
}

func kiemInjectOps(k *KetQuaKiem, c *Config) {
	if len(c.Inject.StripIncoming) > soStripToiDa {
		k.loi("inject.strip_incoming", "tối đa %d mẫu", soStripToiDa)
	}
	for i, p := range c.Inject.StripIncoming {
		if !stripRe.MatchString(p) {
			k.loi(fmt.Sprintf("inject.strip_incoming[%d]", i), "phải khớp ^[a-z0-9*-]{1,64}$ (tên header chữ thường, glob `*`), nhận %q", p)
		}
	}
	if _, _, ok := tachDiaChi(c.Ops.Listen); !ok {
		k.loi("ops.listen", "phải dạng host:port (vd \"127.0.0.1:9090\"), nhận %q", c.Ops.Listen)
	} else if c.Ops.Listen == c.Gate.Listen {
		k.loi("ops.listen", "trùng gate.listen (%s): cổng vận hành phải bind riêng", c.Gate.Listen)
	}
	if c.Ops.LogCustomerRef != "hash" && c.Ops.LogCustomerRef != "none" {
		k.loi("ops.log_customer_ref", "phải là hash hoặc none (không bao giờ log customer_ref thô), nhận %q", c.Ops.LogCustomerRef)
	}
}
