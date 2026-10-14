// Runner conformance `gate-v1` của market-gate: chạy MỌI ca của meta (`contracts/conformance/gate-v1`) qua proxy thật (chay.Lap) với hub giả,
// session-hook giả, upstream giả và đồng hồ giả. Ca lệch = đỏ; không skip; không đọc được bộ ca = đỏ có nhãn "KHÔNG KIỂM ĐƯỢC"
// (README gate-v1, "Hợp đồng cho runner" #3–#4). Bộ ca đọc từ `DZ_META_DIR` (mặc định `../meta` cạnh kho này).
package conformance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Ca là một ca conformance (chỉ các trường runner dùng).
type Ca struct {
	ID          string                `json:"id"`
	Clock       *struct{ Now string } `json:"clock"`
	Config      map[string]any        `json:"config"`
	Hub         CaHub                 `json:"hub"`
	SessionHook *CaSessionHook        `json:"session_hook"`
	// JWKS: trả lời của `identity[].jwks_url` — `{error:{status}}` = JWKS không tải được; vắng ⇒ cũng không tải được (README "Khối jwks").
	JWKS *struct {
		Error struct{ Status int } `json:"error"`
	} `json:"jwks"`
	Steps []Buoc `json:"steps"`
}

type CaHub struct {
	Entitlements map[string]TraLoi `json:"entitlements"`
	Checkout     json.RawMessage   `json:"checkout"`
}

// TraLoi: một trả lời của hub giả, hoặc {sequence: [...]}.
type TraLoi struct {
	Snapshot    json.RawMessage       `json:"snapshot"`
	Conditional bool                  `json:"conditional"`
	DelayMs     int                   `json:"delay_ms"`
	Error       *struct{ Status int } `json:"error"`
	Redirect    string                `json:"redirect"`
	Sequence    []TraLoi              `json:"sequence"`
}

type CaSessionHook struct {
	ByCookie map[string]struct {
		Status  int             `json:"status"`
		JSON    json.RawMessage `json:"json"`
		DelayMs int             `json:"delay_ms"`
	} `json:"by_cookie"`
}

// Buoc là một bước: đúng MỘT trong request | clock_advance_ms | invalidate | parallel.
type Buoc struct {
	Request      *YeuCau `json:"request"`
	Expect       *Expect `json:"expect"`
	ClockAdvance *int64  `json:"clock_advance_ms"`
	Invalidate   *string `json:"invalidate"`
	Parallel     *Song   `json:"parallel"`
}

type YeuCau struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Host    string            `json:"host"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// Song là bước `parallel`: nhiều request đồng thời, hub_calls đếm cho CẢ bước.
type Song struct {
	Requests []struct {
		Request YeuCau `json:"request"`
		Expect  Expect `json:"expect"`
	} `json:"requests"`
	HubCalls *SoGoi `json:"hub_calls"`
}

type SoGoi struct {
	Entitlements *int `json:"entitlements"`
	Checkout     *int `json:"checkout"`
}

type Expect struct {
	Decision       QuyetKy    `json:"decision"`
	Response       *PhanHoiKy `json:"response"`
	UpstreamCalled *bool      `json:"upstream_called"`
	// UpstreamCalledTheoMatTien: kỳ vọng RIÊNG từng mặt tiền (khác biệt có chủ ý, vd `/_dz/*`); runner Go đọc `gate`.
	UpstreamCalledTheoMatTien *struct {
		Gate *bool `json:"gate"`
	} `json:"upstream_called_theo_mat_tien"`
	UpstreamRequestHeaders map[string]string `json:"upstream_request_headers"`
	HubCalls               *SoGoi            `json:"hub_calls"`
	HubRequestHeaders      map[string]string `json:"hub_request_headers"`
	CheckoutRequest        map[string]any    `json:"checkout_request"`
}

type QuyetKy struct {
	Allowed     bool    `json:"allowed"`
	Reason      *string `json:"reason"`
	Status      *int    `json:"status"`
	CustomerRef *string `json:"customer_ref"`
	GraceActive *bool   `json:"grace_active"`
}

type PhanHoiKy struct {
	Status     int               `json:"status"`
	Headers    map[string]string `json:"headers"`
	JSON       map[string]any    `json:"json"`
	JSONAbsent []string          `json:"json_absent"`
}

// ThuMucMeta: `DZ_META_DIR` hoặc `../meta` cạnh kho này (cùng quy ước với sdk).
func ThuMucMeta() string {
	if d := os.Getenv("DZ_META_DIR"); d != "" {
		return d
	}
	return filepath.Join("..", "..", "..", "meta")
}

// NapCa đọc schema.json + mọi cases/*.json. Không đọc được (thiếu thư mục, thiếu schema, ca hỏng, id ≠ tên tệp, 0 ca) ⇒ lỗi ghi rõ —
// KHÔNG im lặng bỏ qua.
func NapCa(metaDir string) ([]Ca, error) {
	goc := filepath.Join(metaDir, "contracts", "conformance", "gate-v1")
	if _, err := os.Stat(filepath.Join(goc, "schema.json")); err != nil {
		return nil, fmt.Errorf("KHÔNG KIỂM ĐƯỢC: không đọc được %s/schema.json (đặt DZ_META_DIR tới kho meta): %w", goc, err)
	}
	ds, err := filepath.Glob(filepath.Join(goc, "cases", "*.json"))
	if err != nil || len(ds) == 0 {
		return nil, fmt.Errorf("KHÔNG KIỂM ĐƯỢC: không có ca nào trong %s/cases (%v)", goc, err)
	}
	sort.Strings(ds)
	var cas []Ca
	for _, f := range ds {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, fmt.Errorf("KHÔNG KIỂM ĐƯỢC: %s: %w", f, err)
		}
		var c Ca
		if err := json.Unmarshal(b, &c); err != nil {
			return nil, fmt.Errorf("KHÔNG KIỂM ĐƯỢC: ca hỏng %s: %w", f, err)
		}
		if want := strings.TrimSuffix(filepath.Base(f), ".json"); c.ID != want {
			return nil, fmt.Errorf("KHÔNG KIỂM ĐƯỢC: %s: id %q khác tên tệp", f, c.ID)
		}
		cas = append(cas, c)
	}
	return cas, nil
}
