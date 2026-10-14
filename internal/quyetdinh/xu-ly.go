package quyetdinh

import (
	"context"
	"net/http"

	"github.com/datazen-vn/market-gate/internal/danhtinh"
	"github.com/datazen-vn/market-gate/internal/hub"
	"github.com/datazen-vn/market-gate/internal/tuyen"
)

// CongHopHeader: header đánh dấu "yêu cầu này đã qua gate một lần": thấy ở yêu cầu đến ⇒ 508 (chặn vòng gate tự proxy chính nó).
const CongHopHeader = "X-DZ-Gate-Hop"

// NguonQuyen là phần của hub mà Cong cần (hub.Khach thoả).
type NguonQuyen interface {
	Lay(ctx context.Context, maKhach string, y hub.YeuCau) (*hub.KetQua, error)
	Huy(maKhach string)
}

// Cong gom danh tính, bảng tuyến, quyền dùng và paywall thành MỘT hàm quyết định cho mỗi yêu cầu. Chưa có proxy: proxy gọi XuLy rồi
// hoặc ghi KetQua.PhanHoi (chặn), hoặc chuyển tiếp upstream với header dựng từ KetQua (cho qua).
type Cong struct {
	Quyen    NguonQuyen
	DanhTinh []danhtinh.Bo
	Bang     *tuyen.Bang
	Paywall  *Paywall
	CheDoLoi string // mặc định của cổng: "closed" | "open"
	// BaoLoi nhận lỗi để log/metric (nil = bỏ). Lỗi KHÔNG chứa cookie/token/customer_ref.
	BaoLoi func(error)
}

// KetQua của XuLy.
type KetQua struct {
	Quyet Quyet
	// PhanHoi: phản hồi gate tự trả; nil ⇒ cho qua upstream.
	PhanHoi *PhanHoi
	// Nguon: giá trị `X-DZ-Entitlement-Source` khi cho qua mà không có kết quả hub (vd danh tính không xác định nhưng fail_mode cho qua).
	Nguon string
}

// cách lấy quyền: theo bản đệm; tươi (khách quay về sau thanh toán); hỏi lại (hub báo đã có thuê bao active ⇒ đọc tươi, cùng trần với tươi).
type cachLay int

const (
	layDem cachLay = iota
	layTuoi
	layHoiLai
)

func (c *Cong) baoLoi(err error) {
	if c.BaoLoi != nil && err != nil {
		c.BaoLoi(err)
	}
}

type luot struct {
	quyet Quyet
	nguon string
}

// XuLy quyết định một yêu cầu. KHÔNG panic/ném khi danh tính hoặc hub lỗi (trả LyDoKhongSan).
func (c *Cong) XuLy(ctx context.Context, r *http.Request) KetQua {
	pass := func(q Quyet, nguon string) KetQua { return KetQua{Quyet: q, Nguon: nguon} }
	// 1. Đã qua gate một lần ⇒ vòng lặp: chặn trước mọi việc khác, không gọi hub/upstream.
	if len(r.Header.Values(CongHopHeader)) > 0 {
		q := Quyet{LyDo: LyDoVong, Status: http.StatusLoopDetected}
		return KetQua{Quyet: q, PhanHoi: phanHoiLoi(r.Method, q.Status, MaVong)}
	}
	// 2. Bypass (webhook, login, trang quay lại thanh toán…) và tuyến không cần kiểm quyền.
	tho := tuyen.DuongDanTho(r.RequestURI)
	if r.RequestURI == "" {
		tho = r.URL.EscapedPath()
	}
	xet := c.Bang.Xet(tho)
	quayVe := LaQuayVe(r.Method, r.URL.RawQuery)
	if xet.Qua {
		// success_path là bypass: khách quay về đó vẫn được làm tươi đệm quyền (chỉ làm tươi: không chặn, không tiêm danh tính, mọi lỗi
		// nuốt — upstream luôn được gọi).
		if quayVe {
			c.lamTuoi(ctx, r)
		}
		return pass(ChoQuaAnDanh(), "")
	}
	route := TuDong(xet.Dong)
	// 3. Nhận diện + kiểm quyền. Chưa nhận diện được thì dừng ở no_identity TRƯỚC khi chạm hub (người lạ không khuếch đại được tải).
	cach := layDem
	if quayVe {
		cach = layTuoi
	}
	dau := c.danhGia(ctx, r, route, cach)
	if dau.quyet.Cho {
		return pass(dau.quyet, dau.nguon)
	}
	pt := c.tuChoi(ctx, r, dau.quyet, route)
	if pt.xungDot && dau.quyet.LyDo == LyDoKhongCoQuyen && dau.quyet.KetQua != nil && !dau.quyet.KetQua.HieuLuc {
		// Hub nói khách ĐÃ có thuê bao active cùng gói nhưng bản đệm của ta nói KHÔNG hiệu lực: bản đệm cũ. Hỏi lại = đọc tươi: chịu
		// trần 1 lần / 5 giây / khách và KHÔNG xoá bản đệm quyền (xoá + hỏi lại mỗi request là vòng gọi hub theo đúng tốc độ request của
		// một khách, đốt hạn mức khoá API — panel an ninh C1). Khách đã hiệu lực thì không bao giờ tới đây (tuChoi không tạo phiên).
		lai := c.danhGia(ctx, r, route, layHoiLai)
		if lai.quyet.Cho {
			return pass(lai.quyet, lai.nguon)
		}
		if lai.quyet.LyDo != LyDoKhongCoQuyen {
			return c.tuChoi(ctx, r, lai.quyet, route).ketQua
		}
	}
	return pt.ketQua
}

// lamTuoi: ở bypass có `checkout_session_id` hợp lệ — giải danh tính (không có Cookie/Authorization thì không gọi gì) rồi đọc tươi.
func (c *Cong) lamTuoi(ctx context.Context, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			c.baoLoi(&hub.Loi{Ma: hub.LoiPhanHoi, ChiTiet: "panic khi làm tươi đệm ở trang quay về"})
		}
	}()
	c.danhGia(ctx, r, Route{Mem: true}, layTuoi)
}

func (c *Cong) danhGia(ctx context.Context, r *http.Request, route Route, cach cachLay) luot {
	cheDo := route.CheDoLoi
	if cheDo == "" {
		cheDo = c.CheDoLoi
	}
	if route.Mem {
		cheDo = "open" // soft không bao giờ chặn: hub chập chờn không làm trang giá lỗi
	}
	ket := danhtinh.GiaiQuyet(ctx, r, c.DanhTinh, c.baoLoi)
	switch ket.Loai {
	case danhtinh.ChuaDangNhap:
		if route.Mem {
			return luot{quyet: ChoQuaAnDanh()}
		}
		return luot{quyet: Quyet{LyDo: LyDoKhongDanhTinh, Status: http.StatusUnauthorized}}
	case danhtinh.KhongBiet:
		// Danh tính KHÔNG xác định được ⇒ KHÔNG BAO GIỜ cho qua ẩn danh trên tuyến cần quyền, kể cả fail_mode open: kẻ tấn công tự gây được
		// lỗi danh tính (cookie dị dạng làm /api/me ném 500, hoặc chậm quá timeout) nên đó là "nút mở cổng". fail_mode chỉ áp cho hub
		// entitlements lỗi. Tuyến soft không bao giờ chặn nên vẫn qua (README gate-v1, ca identity-unavailable-never-fail-open).
		if route.Mem {
			return luot{quyet: ChoQuaAnDanh(), nguon: hub.NguonMoCua}
		}
		return luot{quyet: Quyet{LyDo: LyDoKhongSan, Status: http.StatusServiceUnavailable, Tu: TuDanhTinh, Loi: ket.Loi}}
	}

	ma := ket.MaKhach
	kq, err := c.Quyen.Lay(ctx, ma, hub.YeuCau{TinhNang: route.TinhNang, CheDoLoi: cheDo, Tuoi: cach != layDem})
	if err != nil {
		c.baoLoi(err)
		if route.Mem {
			return luot{quyet: Quyet{Cho: true, MaKhach: ma}, nguon: hub.NguonMoCua}
		}
		return luot{quyet: Quyet{LyDo: LyDoKhongSan, Status: http.StatusServiceUnavailable, Tu: TuHub, Loi: err}}
	}
	// Đọc tươi THẬT (không phải bản đệm do trần) = khách vừa quay về: phiên đã đệm là phiên khách vừa dùng — bỏ dù kết quả là gì (active:
	// phiên đã trả; chưa active: có thể đã bị hub thay/hết hạn). Hỏi lại (sau 409) chỉ bỏ khi `active`.
	if kq.Nguon == hub.NguonMang && (cach == layTuoi || (cach == layHoiLai && kq.HieuLuc)) {
		c.Paywall.Quen(ma)
	}
	if route.Mem {
		return luot{quyet: Quyet{Cho: true, MaKhach: ma, AnHan: kq.AnHan, KetQua: kq}}
	}
	return luot{quyet: QuyetQuyen(kq, route)}
}
