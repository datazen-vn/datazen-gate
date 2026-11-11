// Package cauhinh đọc cấu hình YAML của dz-gate, kiểm MỌI luật (liệt kê hết lỗi để `--check` báo đủ một lượt) và dựng các thành phần
// (hub, danh tính, bảng tuyến, paywall) thành một quyetdinh.Cong.
//
// Hình dạng khớp 1:1 với `config` của bộ ca conformance gate-v1 (snake_case) và với tuỳ chọn `createGate` của @datazen-vn/sdk.
// Bí mật KHÔNG nằm trong YAML: khoá API qua `hub.api_key_env`, secret HMAC qua `secret_env`.
package cauhinh

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"time"

	"gopkg.in/yaml.v3"
)

// ThoiLuong đọc từ chuỗi kiểu `60s`, `500ms`, `5m`, `1h` (cùng khuôn `^[0-9]{1,6}(ms|s|m|h)$` của schema gate-v1).
type ThoiLuong time.Duration

// UnmarshalYAML chỉ nhận scalar chuỗi dạng Go duration; số trần (không đơn vị) bị từ chối để khỏi nhầm đơn vị.
func (t *ThoiLuong) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind != yaml.ScalarNode || n.Tag != "!!str" {
		return fmt.Errorf("dòng %d: thời lượng phải là chuỗi có đơn vị (vd 60s, 500ms, 5m)", n.Line)
	}
	d, err := time.ParseDuration(n.Value)
	if err != nil {
		return fmt.Errorf("dòng %d: thời lượng %q không hợp lệ (vd 60s, 500ms, 5m)", n.Line, n.Value)
	}
	*t = ThoiLuong(d)
	return nil
}

// D trả time.Duration.
func (t ThoiLuong) D() time.Duration { return time.Duration(t) }

// Config là toàn bộ cấu hình.
type Config struct {
	Gate          Gate       `yaml:"gate"`
	Hub           Hub        `yaml:"hub"`
	Identity      []Identity `yaml:"identity"`
	Routes        []Route    `yaml:"routes"`
	DefaultAction string     `yaml:"default_action"`
	Paywall       Paywall    `yaml:"paywall"`
	Inject        Inject     `yaml:"inject"`
	Ops           Ops        `yaml:"ops"`
}

// Gate: nơi gate lắng nghe và upstream (app của dev).
type Gate struct {
	Listen   string `yaml:"listen"`
	Upstream string `yaml:"upstream"`
	Mode     string `yaml:"mode"` // proxy | forward-auth
	// TrustedProxies: CIDR (hoặc IP đơn) của load balancer/CDN đứng ngay trước gate. Rỗng (mặc định) = không tin ai: X-Forwarded-* của
	// client luôn bị xoá, IP khách = kết nối trực tiếp. Chỉ kết nối TỚI TỪ các dải này mới được tin X-Forwarded-For/-Proto.
	TrustedProxies []string `yaml:"trusted_proxies"`
	// ForwardAuth chỉ dùng khi mode = forward-auth: gate không có upstream, chỉ trả lời `GET /_dz/auth` cho proxy của dev.
	ForwardAuth ForwardAuth `yaml:"forward_auth"`
}

// ForwardAuth: cấu hình chế độ forward-auth.
type ForwardAuth struct {
	// AllowFrom: CIDR của proxy được tin gửi X-Forwarded-*/X-Original-*. Bắt buộc khi mode = forward-auth; không có loopback ngầm.
	AllowFrom []string `yaml:"allow_from"`
}

// Hub: kết nối tới hub DataZen.
type Hub struct {
	BaseURL      string     `yaml:"base_url"`
	APIKeyEnv    string     `yaml:"api_key_env"`
	CacheTTL     *ThoiLuong `yaml:"cache_ttl"` // vắng = 60s; 0s = tắt đệm
	StaleIfError ThoiLuong  `yaml:"stale_if_error"`
	FailMode     string     `yaml:"fail_mode"` // closed (mặc định) | open
	Timeout      ThoiLuong  `yaml:"timeout"`
}

// Identity: một resolver danh tính (`type`: jwt | session_hook).
type Identity struct {
	Type string `yaml:"type"`
	// jwt
	JWKSURL      string    `yaml:"jwks_url"`
	Secret       string    `yaml:"secret"`
	SecretEnv    string    `yaml:"secret_env"`
	Issuer       string    `yaml:"issuer"`
	Audience     string    `yaml:"audience"`
	SubjectClaim string    `yaml:"subject_claim"`
	Algorithms   []string  `yaml:"algorithms"`
	From         string    `yaml:"from"` // authorization | cookie:<tên> | header:<tên>
	ClockTol     ThoiLuong `yaml:"clock_tolerance"`
	// session_hook
	URL         string   `yaml:"url"`
	Forward     []string `yaml:"forward"`
	SubjectPath string   `yaml:"subject_path"`
	AllowHosts  []string `yaml:"allow_hosts"`
	// dùng chung
	Timeout  ThoiLuong  `yaml:"timeout"`
	CacheTTL *ThoiLuong `yaml:"cache_ttl"` // session_hook: vắng = 30s; 0s = tắt đệm
}

// Route: một dòng bảng tuyến.
type Route struct {
	Match    string `yaml:"match"`
	Action   string `yaml:"action"` // allow | require | soft
	Feature  string `yaml:"feature"`
	PlanCode string `yaml:"plan_code"`
	FailMode string `yaml:"fail_mode"`
}

// Paywall: trang thanh toán + URL quay về.
type Paywall struct {
	PlanCode      string   `yaml:"plan_code"`
	PublicOrigins []string `yaml:"public_origins"`
	LoginURL      string   `yaml:"login_url"`
	SuccessPath   string   `yaml:"success_path"`
	CancelPath    string   `yaml:"cancel_path"`
}

// Inject: header bổ sung cần xoá khỏi yêu cầu đến (ngoài danh sách luôn xoá, xem XoaMacDinh).
type Inject struct {
	// AllowUnderscoreHeaders: mặc định (false) gate BỎ mọi header có `_` trong tên từ client (như nginx `underscores_in_headers off`):
	// Rack/PHP/WSGI/CGI đổi `_` thành `-` nên `X_Custom` giả được `X-Custom`. true chỉ khi app thật sự cần header gạch dưới; header danh tính
	// (`X_DZ_*`…) vẫn bị xoá theo tên đã chuẩn hoá.
	AllowUnderscoreHeaders bool     `yaml:"allow_underscore_headers"`
	StripIncoming          []string `yaml:"strip_incoming"`
}

// Ops: cổng vận hành (healthz/readyz/metrics) trên bind riêng.
type Ops struct {
	Listen         string `yaml:"listen"`
	LogCustomerRef string `yaml:"log_customer_ref"` // hash (mặc định) | none
}

// Doc đọc YAML (hoặc JSON — tập con của YAML) NGHIÊM: trường lạ là lỗi (gõ sai `fail_mod` không âm thầm bị bỏ qua), nhiều tài liệu
// là lỗi. Chưa điền mặc định: gọi MacDinh.
func Doc(r io.Reader) (*Config, error) {
	b, err := io.ReadAll(io.LimitReader(r, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("đọc cấu hình: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	var c Config
	if err := dec.Decode(&c); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, errors.New("cấu hình rỗng")
		}
		return nil, fmt.Errorf("cấu hình không hợp lệ: %w", err)
	}
	var thua yaml.Node
	if err := dec.Decode(&thua); !errors.Is(err, io.EOF) {
		return nil, errors.New("cấu hình chỉ được có MỘT tài liệu YAML")
	}
	return &c, nil
}

// MacDinh điền giá trị mặc định cho trường vắng. Không điền thứ phải do dev khai (upstream, routes, paywall, identity).
func (c *Config) MacDinh() {
	if c.Gate.Mode == "" {
		c.Gate.Mode = "proxy"
	}
	if c.Hub.BaseURL == "" {
		c.Hub.BaseURL = "https://api.datazen.vn"
	}
	if c.Hub.APIKeyEnv == "" {
		c.Hub.APIKeyEnv = "DZ_API_KEY"
	}
	if c.Hub.CacheTTL == nil {
		ttl := ThoiLuong(60 * time.Second)
		c.Hub.CacheTTL = &ttl
	}
	if c.Hub.FailMode == "" {
		c.Hub.FailMode = "closed"
	}
	if c.Hub.Timeout == 0 {
		c.Hub.Timeout = ThoiLuong(5 * time.Second)
	}
	if c.DefaultAction == "" {
		c.DefaultAction = "allow"
	}
	if c.Ops.Listen == "" {
		c.Ops.Listen = "127.0.0.1:9090"
	}
	if c.Ops.LogCustomerRef == "" {
		c.Ops.LogCustomerRef = "hash"
	}
}
