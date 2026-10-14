package proxy

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/datazen-vn/market-gate/internal/quyetdinh"
	"github.com/datazen-vn/market-gate/internal/tuyen"
)

// QuyetDinh là phần của lõi mà proxy cần (*quyetdinh.Cong thoả).
type QuyetDinh interface {
	XuLy(ctx context.Context, r *http.Request) quyetdinh.KetQua
}

// Cau cấu hình proxy.
type Cau struct {
	Cong     QuyetDinh
	Upstream *url.URL
	// Xoa: mẫu header (chữ thường, glob) xoá khỏi yêu cầu đến trước khi chuyển tiếp (cauhinh.Config.XoaHeader()).
	Xoa []string
	// CongKhai: origin công khai của app (paywall.public_origins). Có ⇒ `X-Forwarded-Host`/`-Proto` chỉ lấy từ ĐÂY: Host của client khớp một
	// origin thì dùng origin đó, không khớp thì dùng origin ĐẦU — không bao giờ chuyển nguyên Host do client chọn (app bật "trust proxy"
	// dựng URL tuyệt đối/link đặt lại mật khẩu theo host kẻ tấn công). Rỗng ⇒ giữ giá trị SetXForwarded (Host client).
	CongKhai []Origin
	// Transport tới upstream; nil ⇒ DungTransport().
	Transport http.RoundTripper
	// QuanSat nhận kết quả của MỌI request (cho log/metric) sau khi xong; không được chặn lâu. nil = bỏ.
	QuanSat func(r *http.Request, kq quyetdinh.KetQua, thoiGian time.Duration)
	// BaoLoiUpstream nhận lỗi tới upstream (đã thành 502). nil = bỏ.
	BaoLoiUpstream func(err error)
}

// Origin công khai: Scheme (http|https) + Host (kèm port nếu có, chữ thường).
type Origin struct{ Scheme, Host string }

type khoaKetQua struct{}

// DungTransport: timeout dial 5 s, chờ header phản hồi 120 s, không nén lại (không sửa phản hồi upstream).
func DungTransport() *http.Transport {
	return &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       60 * time.Second,
		ResponseHeaderTimeout: 120 * time.Second,
		DisableCompression:    true,
	}
}

type handler struct {
	cau Cau
	rp  *httputil.ReverseProxy
}

// Moi dựng http.Handler. Mỗi request: lõi quyết định ⇒ chặn (phản hồi của gate) hoặc chuyển tiếp.
func Moi(c Cau) http.Handler {
	if c.Transport == nil {
		c.Transport = DungTransport()
	}
	h := &handler{cau: c}
	h.rp = &httputil.ReverseProxy{
		Rewrite:   h.viet,
		Transport: c.Transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if c.BaoLoiUpstream != nil {
				c.BaoLoiUpstream(err)
			}
			p := quyetdinh.PhanHoiUpstreamLoi(r.Method)
			p.Ghi(w)
		},
	}
	return h
}

// viet dựng request tới upstream từ request đến. ReverseProxy đã: bỏ header hop-by-hop (kể cả tên nêu trong `Connection`) và
// Forwarded/X-Forwarded-* của client trước khi gọi hàm này. Ta: trỏ tới upstream (giữ NGUYÊN đường dẫn thô + query), đặt X-Forwarded-*
// từ kết nối thật (KHÔNG tin giá trị client gửi), XOÁ header danh tính/quyền của client, rồi TIÊM header của gate.
func (h *handler) viet(pr *httputil.ProxyRequest) {
	kq, _ := pr.In.Context().Value(khoaKetQua{}).(quyetdinh.KetQua)
	pr.SetURL(h.cau.Upstream)
	pr.SetXForwarded()
	if len(h.cau.CongKhai) > 0 {
		o := h.congKhaiCua(pr.In.Host)
		pr.Out.Header.Set("X-Forwarded-Host", o.Host)
		pr.Out.Header.Set("X-Forwarded-Proto", o.Scheme) // gate nghe HTTP sau bộ kết thúc TLS: scheme thật của mặt công khai là của origin
	}
	XoaHeader(pr.Out.Header, h.cau.Xoa)
	TiemHeader(pr.Out.Header, kq)
}

// congKhaiCua: origin khớp Host của request, không khớp ⇒ origin đầu.
func (h *handler) congKhaiCua(host string) Origin {
	host = strings.ToLower(host)
	for _, o := range h.cau.CongKhai {
		if o.Host == host {
			return o
		}
	}
	return h.cau.CongKhai[0]
}

// trongVungDZ: một dạng nào của đường dẫn thô nằm trong `/_dz/*` (cổng vận hành của dz-gate).
func trongVungDZ(r *http.Request) bool {
	tho := tuyen.DuongDanTho(r.RequestURI)
	if r.RequestURI == "" {
		tho = r.URL.EscapedPath()
	}
	for _, d := range tuyen.DangDuongDan(tho) {
		if tuyen.KhopGlob("/_dz/*", d) {
			return true
		}
	}
	return false
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	batDau := time.Now()
	kq := h.cau.Cong.XuLy(r.Context(), r)
	defer func() {
		if h.cau.QuanSat != nil {
			h.cau.QuanSat(r, kq, time.Since(batDau))
		}
	}()
	if kq.PhanHoi != nil {
		kq.PhanHoi.Ghi(w)
		return
	}
	// `/_dz/*` là bypass (quyết định vẫn `allowed`) nhưng thuộc về gate: ở mặt công khai trả 404, không mở một tiền tố không kiểm tới app.
	if trongVungDZ(r) {
		quyetdinh.PhanHoiKhongTimThay(r.Method).Ghi(w)
		return
	}
	h.rp.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), khoaKetQua{}, kq)))
}
