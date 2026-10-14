// Package quyetdinh là lõi quyết định của dz-gate: từ yêu cầu + danh tính + quyền dùng + chính sách tuyến ⇒ Quyet (cùng hình với
// `Decision` của @datazen/sdk và `expect.decision` của bộ ca gate-v1) và phản hồi gate tự trả (302/401/402/503/508).
package quyetdinh

import (
	"github.com/datazen-vn/market-gate/internal/hub"
	"github.com/datazen-vn/market-gate/internal/tuyen"
)

// Lý do chặn (khớp `decision.reason` của gate-v1).
const (
	LyDoKhongDanhTinh = "no_identity"
	LyDoKhongCoQuyen  = "not_entitled"
	LyDoKhongSan      = "unavailable"
	LyDoVong          = "loop"
)

// Ai không trả lời khi LyDo = LyDoKhongSan.
const (
	TuDanhTinh = "identity"
	TuHub      = "hub"
)

// Quyet là quyết định về MỘT yêu cầu. Cho = true ⇒ chuyển tiếp upstream; ngược lại Status là mã HTTP (401/402/503/508).
type Quyet struct {
	Cho    bool
	LyDo   string
	Status int
	// MaKhach: customer_ref đã giải ra; rỗng khi tuyến không cần danh tính (allow/bypass/soft thiếu danh tính) hoặc chưa xác định.
	MaKhach string
	AnHan   bool
	// KetQua là quyền dùng (nil khi Cho mà không có kết quả hub: bypass, fail-open vì danh tính không xác định).
	KetQua      *hub.KetQua
	CheckoutURL string // có khi cổng tạo được phiên thanh toán
	Tu          string // LyDoKhongSan: TuDanhTinh | TuHub
	Loi         error  // LyDoKhongSan: lỗi gốc (không chứa dữ liệu khách)
}

// Route là chính sách của một tuyến (dòng `routes` ⇒ Route; tuyến mặc định `require` không feature = Route{}).
type Route struct {
	Mem      bool // soft: không bao giờ chặn
	TinhNang string
	MaGoi    string
	CheDoLoi string
}

// TuDong chuyển một dòng bảng tuyến thành Route (dòng `allow` không tới đây).
func TuDong(d *tuyen.Dong) Route {
	if d == nil {
		return Route{}
	}
	return Route{Mem: d.HanhDong == tuyen.Mem, TinhNang: d.TinhNang, MaGoi: d.MaGoi, CheDoLoi: d.CheDoLoi}
}

// ChoQuaAnDanh: cho qua không cần danh tính/hub (bypass, tuyến allow, soft thiếu danh tính).
func ChoQuaAnDanh() Quyet { return Quyet{Cho: true} }

// QuyetQuyen: lõi thuần, không I/O. Hub không trả lời mà tuyến khai fail_mode open ⇒ cho qua (upstream thấy
// `X-DZ-Entitlement-Source: fail-open`). Trạng thái lạ KHÔNG đoán: không cho qua và không chặn nhầm thành 402 (khách đã trả tiền có
// thể bị "không có quyền") ⇒ 503.
func QuyetQuyen(kq *hub.KetQua, r Route) Quyet {
	if kq.Nguon == hub.NguonMoCua {
		return Quyet{Cho: true, MaKhach: kq.MaKhach, KetQua: kq}
	}
	if kq.TrangThai == hub.TrangLa {
		loi := error(kq.Loi)
		if kq.Loi == nil {
			loi = &hub.Loi{Ma: hub.LoiPhanHoi, HTTP: 200, ChiTiet: "trạng thái thuê bao lạ"}
		}
		return Quyet{LyDo: LyDoKhongSan, Status: 503, Tu: TuHub, Loi: loi}
	}
	duoc := kq.HieuLuc
	if r.TinhNang != "" {
		duoc = kq.TinhNang != nil && kq.TinhNang.ChoPhep
	}
	if duoc {
		return Quyet{Cho: true, MaKhach: kq.MaKhach, AnHan: kq.AnHan, KetQua: kq}
	}
	return Quyet{LyDo: LyDoKhongCoQuyen, Status: 402, MaKhach: kq.MaKhach, KetQua: kq}
}
