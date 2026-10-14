package conformance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sync"
	"time"
)

const hubBaseURL = "https://hub.conformance.test"

// apiKeyThu: runner đặt DZ_API_KEY của gate dạng dz_test_… (README: `Authorization` tới hub khớp `regex:^Bearer dz_`).
const apiKeyThu = "dz_test_conformance0123456789"

type luotGoi struct {
	loai   string // entitlements | checkout
	ref    string
	header http.Header
	than   map[string]any
}

// HubGia là http.RoundTripper thay cho mạng: hub giả + session-hook giả. Ghi lại mọi lời gọi; hỗ trợ `sequence`, `error`, `redirect`,
// 304 theo If-None-Match, `delay_ms` (chờ THẬT, dừng sớm khi ngữ cảnh bị huỷ như fetch thật bị timeout).
type HubGia struct {
	spec    CaHub
	hook    *CaSessionHook
	hookURL string

	mu     sync.Mutex
	calls  []luotGoi
	served map[string]int
	// loiCauHinh: lỗi của CA/runner (hỏi khách không khai, URL lạ) — đỏ, không phải hành vi của gate.
	loiCauHinh []string
}

func newHubGia(spec CaHub, hook *CaSessionHook, hookURL string) *HubGia {
	return &HubGia{spec: spec, hook: hook, hookURL: hookURL, served: map[string]int{}}
}

func (h *HubGia) soGoi() int { h.mu.Lock(); defer h.mu.Unlock(); return len(h.calls) }

func (h *HubGia) gocTu(from int, loai string) []luotGoi {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []luotGoi
	for _, c := range h.calls[from:] {
		if c.loai == loai {
			out = append(out, c)
		}
	}
	return out
}

func (h *HubGia) loiCH(f string, a ...any) {
	h.mu.Lock()
	h.loiCauHinh = append(h.loiCauHinh, fmt.Sprintf(f, a...))
	h.mu.Unlock()
}

func jsonRes(status int, body any, hdr map[string]string) *http.Response {
	b, _ := json.Marshal(body)
	r := &http.Response{StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)), Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1,
		Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(bytes.NewReader(b)), ContentLength: int64(len(b))}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return r
}

func trong(status int, hdr map[string]string) *http.Response {
	r := &http.Response{StatusCode: status, Status: fmt.Sprintf("%d %s", status, http.StatusText(status)), Proto: "HTTP/1.1", ProtoMajor: 1, ProtoMinor: 1, Header: http.Header{}, Body: http.NoBody}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	return r
}

func cho(ctx context.Context, ms int) error {
	if ms <= 0 {
		return nil
	}
	t := time.NewTimer(time.Duration(ms) * time.Millisecond)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

var duongEnt = regexp.MustCompile(`^/v1/customers/([^/]+)/entitlements$`)

// RoundTrip phân phối theo URL: session hook (khớp NGUYÊN URL cấu hình) hoặc hub.
func (h *HubGia) RoundTrip(req *http.Request) (*http.Response, error) {
	if h.hookURL != "" && req.URL.String() == h.hookURL {
		return h.session(req)
	}
	if req.URL.Scheme+"://"+req.URL.Host != hubBaseURL {
		h.loiCH("gọi tới URL lạ: %s", req.URL.Redacted())
		return nil, fmt.Errorf("không có đường tới %s", req.URL.Host)
	}
	if m := duongEnt.FindStringSubmatch(req.URL.EscapedPath()); m != nil && req.Method == http.MethodGet {
		ref, _ := url.PathUnescape(m[1])
		return h.entitlements(req, ref)
	}
	if req.URL.Path == "/v1/checkout-sessions" && req.Method == http.MethodPost {
		return h.checkout(req)
	}
	h.loiCH("đường dẫn hub lạ: %s %s", req.Method, req.URL.Path)
	return jsonRes(404, map[string]string{"code": "not-found", "message": "x", "trace_id": "t"}, nil), nil
}

func (h *HubGia) entitlements(req *http.Request, ref string) (*http.Response, error) {
	h.mu.Lock()
	h.calls = append(h.calls, luotGoi{loai: "entitlements", ref: ref, header: req.Header.Clone()})
	entry, co := h.spec.Entitlements[ref]
	if !co {
		h.loiCauHinh = append(h.loiCauHinh, fmt.Sprintf("ca không khai hub.entitlements cho %q", ref))
		h.mu.Unlock()
		return jsonRes(404, map[string]string{"code": "customer-not-found", "message": "x", "trace_id": "t"}, nil), nil
	}
	reply := entry
	if len(entry.Sequence) > 0 {
		i := h.served[ref]
		h.served[ref]++
		reply = entry.Sequence[min(i, len(entry.Sequence)-1)]
	}
	h.mu.Unlock()

	switch {
	case reply.Redirect != "":
		return trong(302, map[string]string{"Location": reply.Redirect}), nil
	case reply.Error != nil:
		return jsonRes(reply.Error.Status, map[string]string{"code": "hub-error", "message": "x", "trace_id": "t"}, nil), nil
	}
	if err := cho(req.Context(), reply.DelayMs); err != nil {
		return nil, err
	}
	var snap map[string]any
	_ = json.Unmarshal(reply.Snapshot, &snap)
	etag, _ := snap["etag"].(string)
	hdr := map[string]string{}
	if etag != "" {
		hdr["ETag"] = etag
	}
	if reply.Conditional && etag != "" && req.Header.Get("If-None-Match") == etag {
		return trong(304, hdr), nil
	}
	return jsonRes(200, snap, hdr), nil
}

func (h *HubGia) checkout(req *http.Request) (*http.Response, error) {
	var than map[string]any
	b, _ := io.ReadAll(req.Body)
	_ = json.Unmarshal(b, &than)
	h.mu.Lock()
	h.calls = append(h.calls, luotGoi{loai: "checkout", header: req.Header.Clone(), than: than})
	h.mu.Unlock()
	if len(h.spec.Checkout) == 0 {
		return jsonRes(500, map[string]string{"code": "no-checkout-in-case", "message": "x", "trace_id": "t"}, nil), nil
	}
	var spec map[string]any
	_ = json.Unmarshal(h.spec.Checkout, &spec)
	if e, ok := spec["error"].(map[string]any); ok {
		ma, _ := e["code"].(string) // `error.code` tuỳ chọn (vd 409 thue-bao-dang-hieu-luc); mặc định checkout-error
		if ma == "" {
			ma = "checkout-error"
		}
		return jsonRes(int(e["status"].(float64)), map[string]string{"code": ma, "message": "x", "trace_id": "t"}, nil), nil
	}
	spec["id"] = "cs_conf_1"
	return jsonRes(201, spec, nil), nil
}

func (h *HubGia) session(req *http.Request) (*http.Response, error) {
	var cookie string
	if v := req.Header.Values("Cookie"); len(v) > 0 {
		cookie = v[0]
	}
	var r *struct {
		Status  int             `json:"status"`
		JSON    json.RawMessage `json:"json"`
		DelayMs int             `json:"delay_ms"`
	}
	if h.hook != nil {
		if x, ok := h.hook.ByCookie[cookie]; ok {
			r = &x
		}
	}
	if r == nil {
		return trong(401, nil), nil
	}
	if err := cho(req.Context(), r.DelayMs); err != nil {
		return nil, err
	}
	if len(r.JSON) == 0 {
		return trong(r.Status, nil), nil
	}
	var body any
	_ = json.Unmarshal(r.JSON, &body)
	return jsonRes(r.Status, body, nil), nil
}
