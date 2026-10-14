package hub

import (
	"encoding/json"
	"strings"
	"time"
)

// TrangThai thuê bao theo hub; giá trị SDK/gate chưa biết ⇒ TrangThaiLa (không đoán).
type TrangThai string

const (
	Active    TrangThai = "active"
	PastDue   TrangThai = "past_due"
	Expired   TrangThai = "expired"
	Cancelled TrangThai = "cancelled"
	None      TrangThai = "none"
	TrangLa   TrangThai = "unknown"
)

// GioiHan của một khoá `limits`: nil = không giới hạn.
type GioiHan struct{ GiaTri *int64 }

// Snapshot đã kiểm của phản hồi hub — thứ DUY NHẤT được đưa vào bộ đệm.
type Snapshot struct {
	MaKhach    string
	TrangThai  TrangThai
	TrangLaGoc string // chuỗi status lạ (đã làm sạch, cắt ngắn) khi TrangThai = TrangLa
	MaGoi      string // rỗng = null
	Features   map[string]bool
	Limits     map[string]GioiHan
	HetKy      *time.Time // current_period_end
	HetAnHan   *time.Time // grace_until
	Version    *int64
	ETag       string
}

var trangThaiBiet = map[string]TrangThai{
	"active": Active, "past_due": PastDue, "expired": Expired, "cancelled": Cancelled, "none": None,
}

type thanSnapshot struct {
	CustomerRef *string                    `json:"customer_ref"`
	Status      *string                    `json:"status"`
	PlanCode    *string                    `json:"plan_code"`
	Features    map[string]json.RawMessage `json:"features"`
	Limits      map[string]json.RawMessage `json:"limits"`
	PeriodEnd   *string                    `json:"current_period_end"`
	GraceUntil  *string                    `json:"grace_until"`
	Version     json.RawMessage            `json:"version"`
	ETag        *string                    `json:"etag"`
}

// DocSnapshot đọc thân 200 thành Snapshot. Hub được THÊM trường (đọc lỏng) nhưng gate CHẶT ở thứ quyết định quyền: status là chuỗi,
// customer_ref PHẢI có và khớp ĐÚNG chuỗi đã hỏi (phân biệt hoa/thường — thứ duy nhất bắt được bộ đệm/proxy trộn phản hồi giữa hai
// khách), features toàn boolean, limits đúng dạng, mốc thời gian RFC 3339, version số nguyên ≥ 0. `etagHeader` ưu tiên hơn `etag` ở thân.
func DocSnapshot(than []byte, maKhach, etagHeader string) (*Snapshot, error) {
	var w thanSnapshot
	if err := json.Unmarshal(than, &w); err != nil {
		return nil, loi(LoiPhanHoi, "thân không phải đối tượng JSON")
	}
	if w.Status == nil {
		return nil, loi(LoiPhanHoi, "thiếu hoặc sai kiểu trường `status`")
	}
	if w.CustomerRef == nil {
		return nil, loi(LoiPhanHoi, "thiếu trường `customer_ref` (hợp đồng luôn trả)")
	}
	if *w.CustomerRef != maKhach {
		return nil, loi(LoiPhanHoi, "`customer_ref` trong phản hồi không khớp ref đã hỏi")
	}
	s := &Snapshot{MaKhach: maKhach, Features: map[string]bool{}, Limits: map[string]GioiHan{}}
	if tt, ok := trangThaiBiet[*w.Status]; ok {
		s.TrangThai = tt
	} else {
		s.TrangThai = TrangLa
		s.TrangLaGoc = lamSach(*w.Status)
	}
	if w.PlanCode != nil {
		s.MaGoi = *w.PlanCode
	}
	for k, raw := range w.Features {
		var b bool
		if json.Unmarshal(raw, &b) != nil || string(raw) == "null" {
			return nil, loi(LoiPhanHoi, "`features."+lamSach(k)+"` phải là boolean")
		}
		s.Features[k] = b
	}
	for k, raw := range w.Limits {
		g, ok := docGioiHan(raw)
		if !ok {
			return nil, loi(LoiPhanHoi, "`limits."+lamSach(k)+"` phải là {value: số nguyên >= 0 | null} hoặc số nguyên >= 0")
		}
		s.Limits[k] = g
	}
	var err error
	if s.HetKy, err = docMoc(w.PeriodEnd, "current_period_end"); err != nil {
		return nil, err
	}
	if s.HetAnHan, err = docMoc(w.GraceUntil, "grace_until"); err != nil {
		return nil, err
	}
	if len(w.Version) > 0 && string(w.Version) != "null" {
		var v int64
		if json.Unmarshal(w.Version, &v) != nil || v < 0 {
			return nil, loi(LoiPhanHoi, "`version` phải là số nguyên >= 0")
		}
		s.Version = &v
	}
	switch {
	case etagHopLe(etagHeader):
		s.ETag = etagHeader
	case w.ETag != nil && etagHopLe(*w.ETag):
		s.ETag = *w.ETag
	}
	return s, nil
}

// docGioiHan nhận `{value, unit?}` hoặc số trần (platform từng gieo `{"max_outlets": 3}`); dạng khác ⇒ !ok (fail-closed, không đoán).
func docGioiHan(raw json.RawMessage) (GioiHan, bool) {
	if g, ok := docGiaTriGioiHan(raw); ok {
		return g, true
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil || obj == nil {
		return GioiHan{}, false
	}
	v, co := obj["value"]
	if !co {
		return GioiHan{}, false
	}
	return docGiaTriGioiHan(v)
}

// docGiaTriGioiHan: `null` (không giới hạn) hoặc số nguyên >= 0.
func docGiaTriGioiHan(raw json.RawMessage) (GioiHan, bool) {
	if string(raw) == "null" {
		return GioiHan{}, true
	}
	var n int64
	if json.Unmarshal(raw, &n) != nil || n < 0 {
		return GioiHan{}, false
	}
	return GioiHan{GiaTri: &n}, true
}

func docMoc(p *string, ten string) (*time.Time, error) {
	if p == nil {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, *p)
	if err != nil {
		return nil, loi(LoiPhanHoi, "`"+ten+"` không phải mốc thời gian ISO-8601")
	}
	return &t, nil
}

// etagHopLe: ASCII in được, không khoảng trắng, ≤ 256 ký tự; khác ⇒ bỏ (không dùng làm If-None-Match).
func etagHopLe(s string) bool {
	if s == "" || len(s) > 256 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x21 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func lamSach(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			r = '?'
		}
		b.WriteRune(r)
	}
	out := b.String()
	if len(out) > 64 {
		out = out[:64]
	}
	return out
}
