package proxy

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// tranXFF: XFF dài hơn mức này (byte) bị coi là hỏng và bỏ (chặn header khổng lồ/tấn công CPU).
const tranXFF = 1024

func ipKetNoi(remoteAddr string) (netip.Addr, bool) {
	h, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		h = remoteAddr
	}
	a, err := netip.ParseAddr(h)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.Unmap().WithZone(""), true
}

func (h *handler) tinHop(a netip.Addr) bool {
	for _, p := range h.cau.TrustedProxies {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// ipKhachTuLB: CHỈ khi kết nối trực tiếp thuộc TrustedProxies. Duyệt X-Forwarded-For từ phải sang trái, bỏ hop tin cậy; IP đầu tiên không
// tin là khách (mọi hop đều tin ⇒ hop ngoài cùng bên trái). Header hỏng (token không phải IP, quá dài) ⇒ (zero, false): dùng RemoteAddr.
func (h *handler) ipKhachTuLB(in *http.Request) (netip.Addr, bool) {
	if len(h.cau.TrustedProxies) == 0 {
		return netip.Addr{}, false
	}
	remote, ok := ipKetNoi(in.RemoteAddr)
	if !ok || !h.tinHop(remote) {
		return netip.Addr{}, false
	}
	vals := in.Header.Values("X-Forwarded-For")
	if len(vals) == 0 {
		return netip.Addr{}, false
	}
	gop := strings.Join(vals, ",")
	if len(gop) > tranXFF {
		return netip.Addr{}, false
	}
	toks := strings.Split(gop, ",")
	var khach netip.Addr
	for i := len(toks) - 1; i >= 0; i-- {
		a, ok := phanTichTokenXFF(strings.TrimSpace(toks[i]))
		if !ok {
			return netip.Addr{}, false
		}
		a = a.Unmap().WithZone("")
		khach = a
		if !h.tinHop(a) {
			break
		}
	}
	return khach, true
}

// protoTuLB: X-Forwarded-Proto của LB đã tin (token cuối, do LB gần gate nhất thêm), chỉ nhận http|https.
func (h *handler) protoTuLB(in *http.Request) string {
	remote, ok := ipKetNoi(in.RemoteAddr)
	if !ok || !h.tinHop(remote) {
		return ""
	}
	vals := in.Header.Values("X-Forwarded-Proto")
	if len(vals) == 0 {
		return ""
	}
	toks := strings.Split(strings.Join(vals, ","), ",")
	p := strings.ToLower(strings.TrimSpace(toks[len(toks)-1]))
	if p == "http" || p == "https" {
		return p
	}
	return ""
}

// phanTichTokenXFF: IP trần, hoặc dạng có cổng (`1.2.3.4:80`, `[2001:db8::1]:80` — Azure App Gateway…); bỏ cổng.
func phanTichTokenXFF(s string) (netip.Addr, bool) {
	if a, err := netip.ParseAddr(s); err == nil {
		return a.Unmap().WithZone(""), true
	}
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return ap.Addr().Unmap().WithZone(""), true
	}
	return netip.Addr{}, false
}
