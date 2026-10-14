package hub

import "time"

// Nguon của kết quả quyền: network (vừa hỏi hub), cache, stale (hub lỗi hạ tầng, dùng bản cũ còn trong hạn), fail-open (hub không
// trả lời mà tuyến khai fail_mode open).
const (
	NguonMang  = "network"
	NguonDem   = "cache"
	NguonCu    = "stale"
	NguonMoCua = "fail-open"
)

// NhinTinhNang là cái nhìn theo một khoá `features`/`limits`.
type NhinTinhNang struct {
	Khoa    string
	BatTat  bool
	GioiHan *int64
	// ChoPhep: bật và hạn mức khác 0. Với Nguon = fail-open luôn true.
	ChoPhep bool
}

// KetQua là quyền dùng đã tính theo đồng hồ hiện tại — thứ quyetdinh đọc.
type KetQua struct {
	MaKhach   string
	TrangThai TrangThai
	// HieuLuc: `active` hoặc đang trong ân hạn. Trạng thái lạ ⇒ false; fail-open ⇒ true.
	HieuLuc bool
	// AnHan: `past_due` và grace_until còn ở tương lai.
	AnHan    bool
	MaGoi    string
	Features map[string]bool
	Limits   map[string]GioiHan
	HetKy    *time.Time
	HetAnHan *time.Time
	Version  *int64
	// TinhNang có khi hỏi kèm tính năng.
	TinhNang *NhinTinhNang
	Nguon    string
	// Loi: lỗi đã bị cho qua (stale/fail-open) hoặc trạng thái lạ — để log/metric.
	Loi *Loi
	// TrangLaGoc: chuỗi status lạ khi TrangThai = TrangLa.
	TrangLaGoc string
}

// chieu chiếu Snapshot sang KetQua. Chỉ khi HieuLuc mới phơi features/limits (không rò quyền khi không có quyền). Không tự biến
// `active` thành `expired` theo current_period_end vì đồng hồ máy này có thể lệch hub — hub quyết; bộ đệm tự hết hạn đúng mốc đó.
func chieu(s *Snapshot, tinhNang string, bayGio time.Time, nguon string, loiKem *Loi) *KetQua {
	anHan := s.TrangThai == PastDue && s.HetAnHan != nil && s.HetAnHan.After(bayGio)
	hieuLuc := s.TrangThai == Active || anHan
	kq := &KetQua{
		MaKhach: s.MaKhach, TrangThai: s.TrangThai, HieuLuc: hieuLuc, AnHan: anHan, MaGoi: s.MaGoi,
		Features: map[string]bool{}, Limits: map[string]GioiHan{},
		HetKy: s.HetKy, HetAnHan: s.HetAnHan, Version: s.Version, Nguon: nguon, Loi: loiKem, TrangLaGoc: s.TrangLaGoc,
	}
	if hieuLuc {
		for k, v := range s.Features {
			kq.Features[k] = v
		}
		for k, v := range s.Limits {
			kq.Limits[k] = v
		}
	}
	if tinhNang != "" {
		bat := kq.Features[tinhNang]
		gh := kq.Limits[tinhNang].GiaTri
		kq.TinhNang = &NhinTinhNang{Khoa: tinhNang, BatTat: bat, GioiHan: gh, ChoPhep: bat && (gh == nil || *gh != 0)}
	}
	if kq.Loi == nil && s.TrangThai == TrangLa {
		kq.Loi = &Loi{Ma: LoiPhanHoi, HTTP: 200, ChiTiet: "trạng thái thuê bao lạ \"" + s.TrangLaGoc + "\" — coi như không có quyền, nên nâng dz-gate"}
	}
	return kq
}

// moCua là kết quả khi hub không trả lời mà tuyến khai fail_mode open: cho đi tiếp nhưng ĐÁNH DẤU rõ, kèm lỗi gốc.
func moCua(maKhach, tinhNang string, l *Loi) *KetQua {
	kq := &KetQua{
		MaKhach: maKhach, TrangThai: TrangLa, HieuLuc: true, Features: map[string]bool{}, Limits: map[string]GioiHan{},
		Nguon: NguonMoCua, Loi: l,
	}
	if tinhNang != "" {
		kq.TinhNang = &NhinTinhNang{Khoa: tinhNang, BatTat: true, ChoPhep: true}
	}
	return kq
}

// moc thay đổi trạng thái theo thời gian: hết kỳ (active) hoặc hết ân hạn (past_due); không có ⇒ nil.
func (s *Snapshot) moc() *time.Time {
	switch s.TrangThai {
	case Active:
		return s.HetKy
	case PastDue:
		return s.HetAnHan
	}
	return nil
}

// choQuyen: `active`, hoặc `past_due` có grace_until (đã được moc() chặn).
func (s *Snapshot) choQuyen() bool {
	return s.TrangThai == Active || (s.TrangThai == PastDue && s.HetAnHan != nil)
}
