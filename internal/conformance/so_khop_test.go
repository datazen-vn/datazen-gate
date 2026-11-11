package conformance

import (
	"fmt"
	"net/http"
	"reflect"
	"regexp"
	"strings"

	"github.com/datazen-vn/datazen-gate/internal/quyetdinh"
)

// soHeader: giá trị kỳ vọng là chuỗi KHỚP NGUYÊN VĂN, hoặc `regex:<mẫu>` (khớp một phần, không neo ngầm), hoặc `absent` (header phải vắng).
// So tên header không phân biệt hoa/thường (http.Header.Get).
func soHeader(got http.Header, want map[string]string, nhan string) []string {
	var loi []string
	for ten, quy := range want {
		v, co := "", len(got.Values(ten)) > 0
		if co {
			v = got.Get(ten)
		}
		switch {
		case quy == "absent":
			if co {
				loi = append(loi, fmt.Sprintf("%s: header %s phải vắng, có %q", nhan, ten, v))
			}
		case strings.HasPrefix(quy, "regex:"):
			re, err := regexp.Compile(strings.TrimPrefix(quy, "regex:"))
			if err != nil {
				loi = append(loi, fmt.Sprintf("%s: mẫu regex sai %q: %v", nhan, quy, err))
			} else if !co || !re.MatchString(v) {
				loi = append(loi, fmt.Sprintf("%s: header %s = %q không khớp %s", nhan, ten, v, quy))
			}
		default:
			if !co || v != quy {
				loi = append(loi, fmt.Sprintf("%s: header %s = %q, mong %q", nhan, ten, v, quy))
			}
		}
	}
	return loi
}

// tapCon: `want` là TẬP CON của `got` (toMatchObject): mọi khoá khai phải khớp, khoá khác không kiểm; mảng so từng phần tử.
func tapCon(got, want any) bool {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return false
		}
		for k, wv := range w {
			gv, co := g[k]
			if !co || !tapCon(gv, wv) {
				return false
			}
		}
		return true
	case []any:
		g, ok := got.([]any)
		if !ok || len(g) != len(w) {
			return false
		}
		for i := range w {
			if !tapCon(g[i], w[i]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(got, want)
}

// soQuyet: `decision` là hợp đồng giữa hai mặt tiền — so từng trường đã khai với Quyet nội bộ cùng hình.
func soQuyet(q quyetdinh.Quyet, want QuyetKy) []string {
	var loi []string
	if q.Cho != want.Allowed {
		loi = append(loi, fmt.Sprintf("decision.allowed = %v, mong %v", q.Cho, want.Allowed))
	}
	if want.Reason != nil && (q.Cho || q.LyDo != *want.Reason) {
		loi = append(loi, fmt.Sprintf("decision.reason = %q, mong %q", q.LyDo, *want.Reason))
	}
	if want.Status != nil && (q.Cho || q.Status != *want.Status) {
		loi = append(loi, fmt.Sprintf("decision.status = %d, mong %d", q.Status, *want.Status))
	}
	if want.CustomerRef != nil && q.MaKhach != *want.CustomerRef {
		loi = append(loi, fmt.Sprintf("decision.customer_ref = %q, mong %q", q.MaKhach, *want.CustomerRef))
	}
	if want.GraceActive != nil && (!q.Cho || q.AnHan != *want.GraceActive) {
		loi = append(loi, fmt.Sprintf("decision.grace_active = %v, mong %v", q.AnHan, *want.GraceActive))
	}
	return loi
}
