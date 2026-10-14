package proxy

import (
	"context"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/datazen-vn/market-gate/internal/quyetdinh"
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
	// Transport tới upstream; nil ⇒ DungTransport().
	Transport http.RoundTripper
	// QuanSat nhận kết quả của MỌI request (cho log/metric) sau khi xong; không được chặn lâu. nil = bỏ.
	QuanSat func(r *http.Request, kq quyetdinh.KetQua, thoiGian time.Duration)
	// BaoLoiUpstream nhận lỗi tới upstream (đã thành 502). nil = bỏ.
	BaoLoiUpstream func(err error)
}

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
	XoaHeader(pr.Out.Header, h.cau.Xoa)
	TiemHeader(pr.Out.Header, kq)
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
	h.rp.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), khoaKetQua{}, kq)))
}
