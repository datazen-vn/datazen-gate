package danhtinh

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/datazen-vn/market-gate/internal/dem"
	"github.com/datazen-vn/market-gate/internal/gopgoi"
)

const (
	soPhienDemToiDa     = 5_000
	toiDaThanHook       = 256 << 10
	timeoutHookMacDinh  = 2 * time.Second
	timeoutHookToiDa    = 10 * time.Second
	demHookMacDinh      = 30 * time.Second
	demHookToiDa        = 10 * time.Minute
	soHeaderChuyenToiDa = 8
	soDoanDuongDanToiDa = 5
)

// CauHinhSessionHook: hỏi chính app "người này là ai" bằng cách chuyển tiếp cookie/Authorization tới một endpoint kiểu `/api/me`.
// Dùng khi session của app là opaque (không có JWT để tự kiểm).
type CauHinhSessionHook struct {
	URL         string        // 200 + JSON khi đã đăng nhập, 401/403 khi chưa; loopback hoặc host trong AllowHosts
	Forward     []string      // header của yêu cầu gốc được chuyển tiếp; mặc định cookie, authorization
	SubjectPath string        // đường dẫn chấm tới customer_ref trong JSON, mặc định `user.id`
	Timeout     time.Duration // mặc định 2s, tối đa 10s
	TTL         time.Duration // đệm kết quả "đã đăng nhập"; 0 trong cấu hình = mặc định 30s; dùng TatDem để tắt
	TatDem      bool
	AllowHosts  []string // host (kèm port nếu cần) ngoài loopback được phép gọi, vd `app:3000` trong docker compose
	HTTP        *http.Client
	Now         func() time.Time
}

// SessionHook là Bo hỏi session hook. An toàn:
//   - SSRF + lộ cookie: URL CHỈ là loopback hoặc host khai trong AllowHosts (khớp nguyên tên, không hậu tố/ký tự đại diện); không
//     user:password; không theo chuyển hướng (3xx ⇒ lỗi, không gửi cookie tiếp);
//   - bộ đệm khoá bằng SHA-256 của header đã chuyển tiếp: KHÔNG giữ cookie/token thô trong bộ nhớ. Chỉ đệm "đã đăng nhập"; "chưa
//     đăng nhập" không đệm (đăng nhập xong có hiệu lực ngay);
//   - đường dẫn chủ thể chỉ đi qua thuộc tính của JSON, không `__proto__`/`constructor`/`prototype`;
//   - thông báo lỗi không bao giờ chứa giá trị header chuyển tiếp.
type SessionHook struct {
	url     string
	doan    []string
	forward []string
	timeout time.Duration
	ttl     time.Duration
	http    *http.Client
	now     func() time.Time
	dem     *dem.Dem[string]
	nhom    gopgoi.Nhom[string]
}

// MoiSessionHook kiểm cấu hình rồi dựng Bo.
func MoiSessionHook(cfg CauHinhSessionHook) (*SessionHook, error) {
	u, err := kiemURLHook(cfg.URL, cfg.AllowHosts)
	if err != nil {
		return nil, err
	}
	duong := cfg.SubjectPath
	if duong == "" {
		duong = "user.id"
	}
	doan, err := kiemDuongDanChuThe(duong)
	if err != nil {
		return nil, err
	}
	forward := cfg.Forward
	if len(forward) == 0 {
		forward = []string{"cookie", "authorization"}
	}
	var fw []string
	for _, h := range forward {
		h = strings.ToLower(h)
		if !hookHeaderRe.MatchString(h) {
			return nil, errors.New("session_hook.forward phải là tên header ([a-z0-9-])")
		}
		if !contains(fw, h) {
			fw = append(fw, h)
		}
	}
	if len(fw) > soHeaderChuyenToiDa {
		return nil, fmt.Errorf("session_hook.forward tối đa %d header", soHeaderChuyenToiDa)
	}
	h := &SessionHook{url: u.String(), doan: doan, forward: fw, timeout: cfg.Timeout, ttl: cfg.TTL, http: khongTheoChuyenHuong(cfg.HTTP), now: cfg.Now}
	if h.timeout == 0 {
		h.timeout = timeoutHookMacDinh
	}
	if h.timeout < 0 || h.timeout > timeoutHookToiDa {
		return nil, errors.New("session_hook.timeout phải trong (0, 10s]")
	}
	switch {
	case cfg.TatDem:
		h.ttl = 0
	case h.ttl == 0:
		h.ttl = demHookMacDinh
	case h.ttl < 0 || h.ttl > demHookToiDa:
		return nil, errors.New("session_hook.cache_ttl phải trong [0, 10m]")
	}
	if h.now == nil {
		h.now = time.Now
	}
	h.dem = dem.Moi[string](soPhienDemToiDa, 0, h.now)
	return h, nil
}

func contains(ds []string, s string) bool {
	for _, d := range ds {
		if d == s {
			return true
		}
	}
	return false
}

// NhanDien: không có header nào để chuyển tiếp ⇒ chưa đăng nhập, KHÔNG gọi hook (không khuếch đại tải từ người lạ).
func (s *SessionHook) NhanDien(ctx context.Context, r *http.Request) (string, error) {
	var cacHeader []capHeader
	for _, ten := range s.forward {
		if v := r.Header.Values(ten); len(v) > 0 {
			sep := ", "
			if ten == "cookie" {
				sep = "; "
			}
			cacHeader = append(cacHeader, capHeader{ten, strings.Join(v, sep)})
		}
	}
	if len(cacHeader) == 0 {
		return "", nil
	}
	khoa := bam(cacHeader)
	if m, ok := s.dem.Lay(khoa); ok && s.dem.Tuoi(m) {
		return m.GiaTri, nil
	}
	ma, err, _ := s.nhom.Chay(khoa, func() (string, error) {
		ma, err := s.hoi(ctx, cacHeader)
		if err == nil && ma != "" && s.ttl > 0 {
			s.dem.Dat(khoa, ma, s.now().Add(s.ttl))
		}
		return ma, err
	})
	return ma, err
}

func (s *SessionHook) hoi(ctx context.Context, cacHeader []capHeader) (string, error) {
	ctx, huy := context.WithTimeout(context.WithoutCancel(ctx), s.timeout)
	defer huy()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return "", &LoiHaTang{ChiTiet: "session_hook.url không hợp lệ", Goc: err}
	}
	req.Header.Set("Accept", "application/json")
	for _, c := range cacHeader {
		req.Header.Set(c.ten, c.giaTri)
	}
	res, err := s.http.Do(req)
	if err != nil {
		var ne net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
			return "", &LoiHaTang{ChiTiet: "session hook quá " + s.timeout.String(), Goc: err}
		}
		return "", &LoiHaTang{ChiTiet: "không gọi được session hook", Goc: err}
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden:
		return "", nil
	case res.StatusCode != http.StatusOK:
		return "", &LoiHaTang{ChiTiet: fmt.Sprintf("session hook trả HTTP %d (cần 200, hoặc 401/403 khi chưa đăng nhập; không chuyển hướng)", res.StatusCode)}
	}
	than, err := io.ReadAll(io.LimitReader(res.Body, toiDaThanHook+1))
	if err != nil || len(than) > toiDaThanHook {
		return "", &LoiHaTang{ChiTiet: "đọc thân session hook thất bại hoặc quá 256 KB", Goc: err}
	}
	dec := json.NewDecoder(strings.NewReader(string(than)))
	dec.UseNumber()
	var body any
	if err := dec.Decode(&body); err != nil {
		return "", &LoiHaTang{ChiTiet: "thân session hook không phải JSON", Goc: err}
	}
	return chuTheTuJSON(body, s.doan)
}
