package cauhinh

import "net/netip"

// kiemForwardAuth: luật riêng của gate.mode = forward-auth. Gate không có upstream (khai thì bỏ qua + cảnh báo); allow_from bắt buộc
// (đó là danh sách proxy được tin gửi X-Forwarded-*, không có thì ai tới được cổng cũng giả được URL gốc); paywall.public_origins bắt
// buộc vì Host của request ảo phải khớp một origin công khai.
func kiemForwardAuth(k *KetQuaKiem, c *Config) {
	if c.Gate.Upstream != "" {
		k.canh("gate.upstream", "forward-auth không có upstream, giá trị này bị bỏ qua")
	}
	if len(c.Gate.ForwardAuth.AllowFrom) == 0 {
		k.loi("gate.forward_auth.allow_from", "bắt buộc khi gate.mode = forward-auth (CIDR của proxy, vd [\"172.16.0.0/12\"])")
	}
	for _, s := range c.Gate.ForwardAuth.AllowFrom {
		if _, err := netip.ParsePrefix(s); err != nil {
			k.loi("gate.forward_auth.allow_from", "%q không phải CIDR hợp lệ (vd 10.0.0.0/8, 127.0.0.1/32)", s)
		}
	}
	if len(c.Paywall.PublicOrigins) == 0 {
		k.loi("paywall.public_origins", "bắt buộc khi gate.mode = forward-auth (Host của request ảo phải khớp một origin)")
	}
}
