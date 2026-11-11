package proxy

import (
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/datazen-vn/datazen-gate/internal/quyetdinh"
)

// DuongForwardAuth: đường dẫn duy nhất của chế độ forward-auth.
const DuongForwardAuth = "/_dz/auth"

// HeaderCheStatus: nginx auth_request chỉ hiểu 2xx/401/403 (mã khác ⇒ 500, mất 302/402/503 và thân JSON). Request đặt header này = 1 thì
// phản hồi CHẶN của lõi bị đổi thành 401 rỗng, mã thật nằm ở HeaderStatusThat; nginx rồi hỏi lại không có header để lấy nguyên phản hồi.
const (
	HeaderCheStatus  = "X-DZ-Auth-Mask"
	HeaderStatusThat = "X-DZ-Auth-Status"
)

// CauForwardAuth cấu hình handler forward-auth (không có upstream).
type CauForwardAuth struct {
	Cong QuyetDinh
	// Xoa: mẫu header xoá khỏi request ảo trước khi vào lõi (cùng cauhinh.Config.XoaHeader() với chế độ proxy).
	Xoa []string
	// TinTu: mạng của proxy được tin gửi X-Forwarded-*/X-Original-* (gate.forward_auth.allow_from). Rỗng ⇒ không tin ai.
	TinTu []netip.Prefix
	// CongKhai: origin công khai; Host của request ảo phải khớp một origin.
	CongKhai []Origin
	// QuanSat như Cau.QuanSat.
	QuanSat func(r *http.Request, kq quyetdinh.KetQua, thoiGian time.Duration)
}

type forwardAuth struct{ cau CauForwardAuth }

// MoiForwardAuth dựng handler cho `GET /_dz/auth` (Caddy forward_auth, Traefik ForwardAuth, nginx auth_request): dựng request ảo từ
// X-Forwarded-Method/-Host/-Uri (Traefik, Caddy) hoặc X-Original-URI/-Method (nginx) rồi hỏi ĐÚNG lõi quyết định của chế độ proxy.
// Cho qua ⇒ 200 + X-DZ-*; chặn ⇒ cùng phản hồi (302/401/402/503/508) như chế độ proxy.
func MoiForwardAuth(c CauForwardAuth) http.Handler { return &forwardAuth{cau: c} }

func (h *forwardAuth) ghi(w http.ResponseWriter, r *http.Request, bd time.Time, kq quyetdinh.KetQua) {
	if h.cau.QuanSat != nil {
		h.cau.QuanSat(r, kq, time.Since(bd))
	}
	if kq.PhanHoi != nil {
		kq.PhanHoi.Ghi(w)
	}
}

func (h *forwardAuth) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bd := time.Now()
	if r.URL.EscapedPath() != DuongForwardAuth {
		quyetdinh.PhanHoiKhongTimThay(r.Method).Ghi(w)
		return
	}
	if !h.tin(r.RemoteAddr) {
		ph := quyetdinh.PhanHoiForwardKhongTin(r.Method)
		h.ghi(w, r, bd, quyetdinh.KetQua{Quyet: quyetdinh.Quyet{LyDo: quyetdinh.LyDoForwardKhongTin, Status: ph.Status}, PhanHoi: ph})
		return
	}
	// Chỉ nginx (không có X-Forwarded-Method) được che: client qua Caddy/Traefik tự gửi header này không làm mất 302/402 của chính họ.
	che := r.Header.Get(HeaderCheStatus) == "1" && len(r.Header.Values("X-Forwarded-Method")) == 0 // đọc TRƯỚC khi dungRequestAo xoá X-DZ-*
	vr := h.dungRequestAo(r)
	if vr == nil {
		ph := quyetdinh.PhanHoiForwardSai(r.Method)
		h.ghi(w, r, bd, quyetdinh.KetQua{Quyet: quyetdinh.Quyet{LyDo: quyetdinh.LyDoForwardSai, Status: ph.Status}, PhanHoi: ph})
		return
	}
	kq := h.cau.Cong.XuLy(r.Context(), vr)
	if kq.PhanHoi != nil {
		if che {
			kq.PhanHoi = &quyetdinh.PhanHoi{Status: http.StatusUnauthorized, Header: http.Header{
				HeaderStatusThat: {strconv.Itoa(kq.PhanHoi.Status)}, "Cache-Control": {"no-store"}}}
		}
		h.ghi(w, vr, bd, kq)
		return
	}
	// Cho qua: header cho proxy chép xuống app. `X-DZ-Gate-Hop` chỉ để chặn vòng proxy ⇒ không có nghĩa ở đây.
	hd := http.Header{}
	TiemHeader(hd, kq)
	hd.Del(quyetdinh.CongHopHeader)
	for k, v := range hd {
		w.Header()[k] = v
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	h.ghi(w, vr, bd, kq)
}

// tin: địa chỉ kết nối THẬT (RemoteAddr) thuộc allow_from. Không bao giờ đọc X-Forwarded-For.
func (h *forwardAuth) tin(remote string) bool {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	ip = ip.Unmap()
	for _, p := range h.cau.TinTu {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// dungRequestAo dựng request mà khách thật đã gửi tới proxy: giữ nguyên header (Cookie/Authorization/Accept/Sec-Fetch-Mode… do proxy
// chép sang) nhưng đổi method/host/đường dẫn thô theo header của proxy. nil ⇒ thiếu/hỏng.
func (h *forwardAuth) dungRequestAo(r *http.Request) *http.Request {
	uri := dau(r.Header, "X-Forwarded-Uri", "X-Original-Uri")
	host := strings.ToLower(dau(r.Header, "X-Forwarded-Host", "X-Original-Host"))
	if !uriHopLe(uri) || !h.khopCongKhai(host) {
		return nil
	}
	method := strings.ToUpper(dau(r.Header, "X-Forwarded-Method", "X-Original-Method"))
	if method == "" {
		method = r.Method // nginx auth_request giữ method gốc
	}
	if !tokenHopLe(method) {
		return nil
	}
	u, err := url.ParseRequestURI(uri)
	if err != nil {
		return nil
	}
	vr := r.Clone(r.Context())
	vr.Method, vr.URL, vr.RequestURI, vr.Host = method, u, uri, host
	// Header proxy đã dùng xong; xoá luôn mọi thứ client tự đặt (X-DZ-*, X-Forwarded-*, X-Original-*…) khỏi những gì lõi nhìn thấy.
	XoaHeader(vr.Header, h.cau.Xoa)
	return vr
}

func (h *forwardAuth) khopCongKhai(host string) bool {
	if host == "" {
		return false
	}
	for _, o := range h.cau.CongKhai {
		if o.Host == host {
			return true
		}
	}
	return false
}

// dau: giá trị của header đầu tiên có mặt. Header lặp (nhiều giá trị) ⇒ lấy "" vì không biết tin cái nào.
func dau(h http.Header, ten ...string) string {
	for _, t := range ten {
		if v := h.Values(t); len(v) == 1 {
			return strings.TrimSpace(v[0])
		} else if len(v) > 1 {
			return ""
		}
	}
	return ""
}

// uriHopLe: dạng origin-form (`/đường?query`), không ký tự điều khiển/khoảng trắng; không `//` đầu (dạng `//host` bị hiểu là authority).
func uriHopLe(s string) bool {
	if len(s) < 1 || len(s) > 8192 || s[0] != '/' || strings.HasPrefix(s, "//") {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] <= 0x20 || s[i] == 0x7f {
			return false
		}
	}
	return true
}

func tokenHopLe(m string) bool {
	if m == "" || len(m) > 16 {
		return false
	}
	for i := 0; i < len(m); i++ {
		if m[i] < 'A' || m[i] > 'Z' {
			return false
		}
	}
	return true
}
