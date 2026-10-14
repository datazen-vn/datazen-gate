// Package proxy là reverse proxy của dz-gate: hỏi lõi quyết định (quyetdinh.Cong) cho MỖI request, chặn thì tự trả phản hồi, cho qua
// thì chuyển tiếp upstream với header đã DỌN rồi TIÊM. Không bao giờ sửa phản hồi của upstream.
package proxy

import (
	"net/http"
	"sort"
	"strings"

	"github.com/datazen-vn/market-gate/internal/hub"
	"github.com/datazen-vn/market-gate/internal/quyetdinh"
	"github.com/datazen-vn/market-gate/internal/tuyen"
)

// XoaHeader xoá khỏi `h` mọi header khớp một mẫu glob (so tên KHÔNG phân biệt hoa/thường; mẫu viết thường). Dùng để loại header
// danh tính/quyền do CLIENT tự đặt (`X-DZ-*`, `X-Auth-Request-*`, `X-Forwarded-User`, + strip_incoming): upstream tin `X-DZ-*` nên
// client KHÔNG được giả.
func XoaHeader(h http.Header, mau []string) {
	for ten := range h {
		l := strings.ToLower(ten)
		for _, m := range mau {
			if tuyen.KhopGlob(m, l) {
				delete(h, ten)
				break
			}
		}
	}
}

func giaTriAnToan(v string) bool {
	if len(v) < 1 || len(v) > 512 {
		return false
	}
	for i := 0; i < len(v); i++ {
		if v[i] < 0x20 || v[i] > 0x7e {
			return false
		}
	}
	return true
}

func tenTinhNangAnToan(k string) bool {
	if k == "" {
		return false
	}
	for i := 0; i < len(k); i++ {
		if k[i] <= 0x20 || k[i] == 0x7f || k[i] == ',' {
			return false
		}
	}
	return true
}

// TiemHeader đặt header của gate lên request chuyển tiếp: X-DZ-Customer-Ref, -Entitlement-Status, -Plan, -Features (csv các khoá
// bật, sắp xếp), -Grace-Until (RFC 3339 UTC, chỉ khi đang trong ân hạn), -Entitlement-Source, và X-DZ-Gate-Hop: 1 (chặn vòng).
// Giá trị lạ (hub trả bậy) bị BỎ thay vì làm hỏng header. Gọi SAU XoaHeader.
func TiemHeader(h http.Header, kq quyetdinh.KetQua) {
	dat := func(ten, v string) {
		if giaTriAnToan(v) {
			h.Set(ten, v)
		}
	}
	q := kq.Quyet
	dat("X-DZ-Customer-Ref", q.MaKhach)
	if e := q.KetQua; e != nil {
		dat("X-DZ-Entitlement-Status", string(e.TrangThai))
		dat("X-DZ-Plan", e.MaGoi)
		dat("X-DZ-Features", cacTinhNang(e))
		if e.AnHan && e.HetAnHan != nil {
			dat("X-DZ-Grace-Until", quyetdinh.DinhDangMoc(*e.HetAnHan))
		}
		dat("X-DZ-Entitlement-Source", e.Nguon)
	} else {
		dat("X-DZ-Entitlement-Source", kq.Nguon)
	}
	h.Set(quyetdinh.CongHopHeader, "1")
}

func cacTinhNang(e *hub.KetQua) string {
	var ds []string
	for k, bat := range e.Features {
		if bat && tenTinhNangAnToan(k) {
			ds = append(ds, k)
		}
	}
	sort.Strings(ds)
	return strings.Join(ds, ",")
}
