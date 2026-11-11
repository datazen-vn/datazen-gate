// Package proxy là reverse proxy của dz-gate: hỏi lõi quyết định (quyetdinh.Cong) cho MỖI request, chặn thì tự trả phản hồi, cho qua
// thì chuyển tiếp upstream với header đã DỌN rồi TIÊM. Không bao giờ sửa phản hồi của upstream.
package proxy

import (
	"net/http"
	"sort"
	"strings"

	"github.com/datazen-vn/datazen-gate/internal/hub"
	"github.com/datazen-vn/datazen-gate/internal/quyetdinh"
	"github.com/datazen-vn/datazen-gate/internal/tuyen"
)

// ChuanHoaTenHeader: hạ chữ thường VÀ đổi `_` thành `-`. Server theo quy ước CGI (Rack/Puma/Unicorn, PHP-FPM, WSGI cũ, Gunicorn < 22)
// đổi cả hai dấu thành `HTTP_X_DZ_CUSTOMER_REF`, nên `X_DZ_Customer_Ref` mà lọt qua thì upstream đọc nó như `X-DZ-Customer-Ref`
// (panel an ninh gate #2, C1-PR2; README gate-v1 "Tên header").
func ChuanHoaTenHeader(ten string) string {
	return strings.ReplaceAll(strings.ToLower(ten), "_", "-")
}

// XoaHeader xoá khỏi `h` mọi header khớp một mẫu glob; tên header VÀ mẫu đều được ChuanHoaTenHeader trước khi so. Dùng để loại header
// danh tính/quyền do CLIENT tự đặt (`X-DZ-*`, `X-Auth-Request-*`, `X-Forwarded-User`, + strip_incoming): upstream tin `X-DZ-*` nên
// client KHÔNG được giả.
func XoaHeader(h http.Header, mau []string) {
	chuan := make([]string, len(mau))
	for i, m := range mau {
		chuan[i] = ChuanHoaTenHeader(m)
	}
	for ten := range h {
		l := ChuanHoaTenHeader(ten)
		for _, m := range chuan {
			if tuyen.KhopGlob(m, l) {
				delete(h, ten)
				break
			}
		}
	}
}

// BoHeaderGachDuoi bỏ mọi header có `_` trong tên (như nginx `underscores_in_headers off`): server CGI-style đổi `_` thành `-` nên
// `X_Custom` mạo được `X-Custom`; chuẩn hoá tên ở XoaHeader chỉ phủ nhóm danh tính/quyền đã biết, còn header của chính app thì không biết
// được — cách chắc là không cho gạch dưới đi qua (trừ khi vận hành khai `inject.allow_underscore_headers`).
func BoHeaderGachDuoi(h http.Header) {
	for ten := range h {
		if strings.Contains(ten, "_") {
			delete(h, ten)
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
