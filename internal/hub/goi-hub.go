package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const toiDaThan = 256 << 10 // thân phản hồi ≤ 256 KB

var (
	maHubRe = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	uuidRe  = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

type phanHoi struct {
	trangThai int
	than      []byte
	etag      string
	khongDoi  bool // 304 khi đã gửi If-None-Match
}

// yeuCau mô tả một lời gọi hub. `duongDan` đã percent-encode (RawPath); `duongDanGiai` là dạng giải mã tương ứng.
type yeuCau struct {
	phuongThuc   string
	duongDan     string
	duongDanGiai string
	than         []byte
	ifNoneMatch  string
	anhXaLoi4xx  func(*Loi) *Loi // ánh xạ riêng cho 4xx ngoài 401/429 (vd checkout ⇒ dz_checkout_failed)
}

// clientKhongTheoChuyenHuong dùng lại Transport của `goc` nhưng KHÔNG theo chuyển hướng: lời gọi mang apiKey chỉ được tới đúng
// base_url. 3xx được trả về nguyên để phân biệt "cấu hình sai" (dz_redirect_refused, không thử lại) với lỗi mạng thật.
func clientKhongTheoChuyenHuong(goc *http.Client) *http.Client {
	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if goc != nil {
		c.Transport = goc.Transport
		c.Jar = nil
	}
	return c
}

// maHoaThanhPhan giống encodeURIComponent của JS: chỉ chừa `A-Za-z0-9 - _ . ! ~ * ' ( )`. customer_ref nằm trong ĐƯỜNG DẪN nên
// mọi ký tự khác (`/ ? # %`…) phải bị mã hoá.
func maHoaThanhPhan(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || strings.IndexByte("-_.!~*'()", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}

func (k *Khach) gui(ctx context.Context, y yeuCau) (*phanHoi, error) {
	u, err := url.Parse(k.cfg.BaseURL)
	if err != nil {
		return nil, &Loi{Ma: LoiCauHinh, ChiTiet: "base_url không hợp lệ", Goc: err}
	}
	u.Path, u.RawPath = y.duongDanGiai, y.duongDan
	var thanReader io.Reader
	if y.than != nil {
		thanReader = bytes.NewReader(y.than)
	}
	// Bỏ huỷ của người gọi: lời gọi hub được GỘP giữa nhiều request nên không để một client ngắt kết nối làm hỏng lần chạy chung.
	ctxGoi, huy := context.WithTimeout(context.WithoutCancel(ctx), k.cfg.Timeout)
	defer huy()
	req, err := http.NewRequestWithContext(ctxGoi, y.phuongThuc, u.String(), thanReader)
	if err != nil {
		return nil, &Loi{Ma: LoiCauHinh, ChiTiet: "không dựng được yêu cầu hub", Goc: err}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+k.cfg.APIKey)
	req.Header.Set("User-Agent", k.cfg.UserAgent)
	if y.than != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if y.ifNoneMatch != "" {
		req.Header.Set("If-None-Match", y.ifNoneMatch)
	}
	res, err := k.http.Do(req)
	if err != nil {
		var ne net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
			return nil, &Loi{Ma: LoiHetGio, ChiTiet: "quá " + k.cfg.Timeout.String(), Goc: err}
		}
		return nil, &Loi{Ma: LoiMang, ChiTiet: "gọi hub thất bại", Goc: err}
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotModified && y.ifNoneMatch != "" {
		return &phanHoi{trangThai: 304, khongDoi: true, etag: res.Header.Get("ETag")}, nil
	}
	if res.StatusCode >= 300 && res.StatusCode < 400 {
		return nil, &Loi{Ma: LoiChuyenHuong, HTTP: res.StatusCode, ChiTiet: "hub trả chuyển hướng; cổng không theo (kiểm tra hub.base_url)"}
	}
	than, err := io.ReadAll(io.LimitReader(res.Body, toiDaThan+1))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return nil, &Loi{Ma: LoiHetGio, ChiTiet: "quá " + k.cfg.Timeout.String() + " khi đọc thân", Goc: err}
		}
		return nil, &Loi{Ma: LoiMang, HTTP: res.StatusCode, ChiTiet: "đọc thân phản hồi thất bại", Goc: err}
	}
	if len(than) > toiDaThan {
		if res.StatusCode >= 200 && res.StatusCode < 300 {
			return nil, &Loi{Ma: LoiPhanHoi, HTTP: res.StatusCode, ChiTiet: "thân phản hồi quá 256 KB"}
		}
		than = nil // thân lỗi quá trần (vd trang HTML của proxy): vẫn ánh xạ theo trạng thái HTTP
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, loiTuTrangThai(res.StatusCode, than, y.anhXaLoi4xx)
	}
	return &phanHoi{trangThai: res.StatusCode, than: than, etag: res.Header.Get("ETag")}, nil
}

// loiTuTrangThai ánh xạ trạng thái HTTP không phải 2xx sang Loi. 401 = khoá sai; 429 = hạn mức; 5xx = hạ tầng; 4xx khác: theo
// ánh xạ riêng của lời gọi (nếu có) hoặc dz_bad_request (400/422) / dz_unexpected_status.
func loiTuTrangThai(tt int, than []byte, anhXa func(*Loi) *Loi) *Loi {
	l := &Loi{HTTP: tt}
	l.HubCode, l.PhienLienQuan = docThanLoi(than)
	switch {
	case tt == http.StatusUnauthorized:
		l.Ma = LoiKhoa
	case tt == http.StatusTooManyRequests:
		l.Ma = LoiHanMuc
	case tt >= 500:
		l.Ma = LoiMayChu
	default:
		l.Ma = LoiTrangThai
		if tt == 400 || tt == 422 {
			l.Ma = LoiYeuCau
		}
		if anhXa != nil {
			l = anhXa(l)
		}
		l.ChiTiet = "HTTP " + http.StatusText(tt)
	}
	return l
}

// docThanLoi lấy `code` (kebab-case, ký tự an toàn) và `details.checkout_session_id` (uuid) của thân lỗi hub.
func docThanLoi(than []byte) (maHub, phien string) {
	var w struct {
		Code    string `json:"code"`
		Details struct {
			Phien string `json:"checkout_session_id"`
		} `json:"details"`
	}
	if len(than) == 0 || json.Unmarshal(than, &w) != nil {
		return "", ""
	}
	if maHubRe.MatchString(w.Code) {
		maHub = w.Code
	}
	if uuidRe.MatchString(w.Details.Phien) {
		phien = w.Details.Phien
	}
	return maHub, phien
}
