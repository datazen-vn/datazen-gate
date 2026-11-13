package cauhinh

import "net/netip"

// ParseTrustedProxies đổi danh sách CIDR/IP đơn thành prefix (IP đơn = /32 hoặc /128; IPv4-mapped IPv6 được gỡ về IPv4).
// Dùng chung cho Kiem (báo lỗi) và chay.Lap (dựng proxy). Lỗi ở phần tử nào trả kèm chỉ số.
func ParseTrustedProxies(ds []string) ([]netip.Prefix, []int) {
	var ps []netip.Prefix
	var hong []int
	for i, s := range ds {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			a, err2 := netip.ParseAddr(s)
			if err2 != nil || a.Zone() != "" {
				hong = append(hong, i)
				continue
			}
			p = netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen())
		}
		if p.Addr().Zone() != "" {
			hong = append(hong, i)
			continue
		}
		ps = append(ps, p.Masked())
	}
	return ps, hong
}

// kiemTrustedProxies: CIDR hợp lệ; cảnh báo khi dải tin MỌI người (/0) — client nào cũng giả được IP.
func kiemTrustedProxies(k *KetQuaKiem, ds []string) {
	ps, hong := ParseTrustedProxies(ds)
	for _, i := range hong {
		k.loi("gate.trusted_proxies", "phần tử [%d] %q không phải CIDR/IP hợp lệ (vd 10.0.0.0/8, 2001:db8::/32)", i, ds[i])
	}
	for _, p := range ps {
		if p.Bits() == 0 {
			k.canh("gate.trusted_proxies", "%s tin MỌI địa chỉ: client nào cũng giả được IP (X-Forwarded-For) và scheme; chỉ khai dải của LB/CDN thật", p)
		}
	}
}
