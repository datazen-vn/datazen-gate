package hub

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDocSnapshotHopLe(t *testing.T) {
	than, _ := json.Marshal(snap("cus_1", "active", map[string]any{
		"plan_code": "pro", "features": map[string]bool{"premium": true, "export": false},
		"limits":             map[string]any{"seats": map[string]any{"value": 5, "unit": "ghe"}, "outlets": 3, "api": map[string]any{"value": nil}},
		"current_period_end": "2026-11-13T17:00:00+07:00", "grace_until": nil, "version": 7, "etag": `W/"7:active"`,
	}))
	s, err := DocSnapshot(than, "cus_1", `W/"header"`)
	if err != nil {
		t.Fatal(err)
	}
	if s.TrangThai != Active || s.MaGoi != "pro" || !s.Features["premium"] || s.Features["export"] {
		t.Fatalf("snapshot sai: %+v", s)
	}
	if s.ETag != `W/"header"` {
		t.Fatalf("ETag header phải thắng thân: %q", s.ETag)
	}
	if g := s.Limits["seats"]; g.GiaTri == nil || *g.GiaTri != 5 {
		t.Fatalf("limits.seats: %+v", g)
	}
	if g := s.Limits["outlets"]; g.GiaTri == nil || *g.GiaTri != 3 {
		t.Fatalf("limits.outlets (số trần): %+v", g)
	}
	if g, ok := s.Limits["api"]; !ok || g.GiaTri != nil {
		t.Fatalf("limits.api null = không giới hạn: %+v", g)
	}
	// Mốc có múi giờ so theo thời điểm, không theo chuỗi.
	if !s.HetKy.Equal(time.Date(2026, 11, 13, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("current_period_end = %v", s.HetKy)
	}
	if *s.Version != 7 {
		t.Fatalf("version = %v", *s.Version)
	}
}

func TestDocSnapshotETagTuThan(t *testing.T) {
	than, _ := json.Marshal(snap("cus_1", "none", map[string]any{"etag": `W/"1:none"`}))
	s, err := DocSnapshot(than, "cus_1", "")
	if err != nil || s.ETag != `W/"1:none"` {
		t.Fatalf("etag từ thân: %v %+v", err, s)
	}
	// ETag header sai hình (có khoảng trắng) bị bỏ, rơi về thân.
	s, err = DocSnapshot(than, "cus_1", "a b")
	if err != nil || s.ETag != `W/"1:none"` {
		t.Fatalf("etag header hỏng: %v %+v", err, s)
	}
}

func TestDocSnapshotTrangThaiLa(t *testing.T) {
	than, _ := json.Marshal(snap("cus_1", "paused\x01zz", nil))
	s, err := DocSnapshot(than, "cus_1", "")
	if err != nil || s.TrangThai != TrangLa || s.TrangLaGoc != "paused?zz" {
		t.Fatalf("status lạ phải thành unknown, đã làm sạch: %v %+v", err, s)
	}
}

func TestDocSnapshotTuChoi(t *testing.T) {
	cacTruongHop := []struct {
		ten  string
		than string
	}{
		{"không phải JSON", `nope`},
		{"là mảng", `[]`},
		{"thiếu status", `{"customer_ref":"cus_1"}`},
		{"status sai kiểu", `{"customer_ref":"cus_1","status":5}`},
		{"thiếu customer_ref", `{"status":"none"}`},
		{"customer_ref lệch hoa/thường", `{"customer_ref":"CUS_1","status":"none"}`},
		{"customer_ref của khách khác", `{"customer_ref":"cus_2","status":"active"}`},
		{"features không boolean", `{"customer_ref":"cus_1","status":"active","features":{"x":"yes"}}`},
		{"features null", `{"customer_ref":"cus_1","status":"active","features":{"x":null}}`},
		{"limits âm", `{"customer_ref":"cus_1","status":"active","limits":{"x":-1}}`},
		{"limits thiếu value", `{"customer_ref":"cus_1","status":"active","limits":{"x":{"unit":"u"}}}`},
		{"limits chuỗi", `{"customer_ref":"cus_1","status":"active","limits":{"x":"3"}}`},
		{"mốc hỏng", `{"customer_ref":"cus_1","status":"active","current_period_end":"hôm qua"}`},
		{"version âm", `{"customer_ref":"cus_1","status":"active","version":-2}`},
		{"version là chuỗi", `{"customer_ref":"cus_1","status":"active","version":"2"}`},
	}
	for _, c := range cacTruongHop {
		_, err := DocSnapshot([]byte(c.than), "cus_1", "")
		l, ok := ALoi(err)
		if !ok || l.Ma != LoiPhanHoi {
			t.Errorf("%s: mong dz_bad_response, có %v", c.ten, err)
		}
	}
}
