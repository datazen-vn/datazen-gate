package hub

import (
	"context"
	"net/http"
)

// dangKy (giữ k.mu) trả lần bay cho `ma`: nhập vào lần đang bay, hoặc mở lần mới. Lời gọi `tuoi` chỉ nhập vào lần bay cũng do
// `tuoi` bắt đầu: lần bay bắt đầu TRƯỚC webhook có thể mang câu trả lời cũ.
func (k *Khach) dangKy(ma string, tuoi bool) (*luotBay, bool) {
	if b, ok := k.dangBay[ma]; ok && (!tuoi || b.tuoi) {
		return b, false
	}
	b := &luotBay{xong: make(chan struct{}), tuoi: tuoi}
	k.dangBay[ma] = b
	return b, true
}

// chay: người nhập vào thì ĐỢI kết quả chung; người mở lần bay thì gọi hub rồi ghi đệm.
func (k *Khach) chay(ctx context.Context, ma string, truoc *Snapshot, b *luotBay, laChu bool) (*Snapshot, error) {
	if !laChu {
		select {
		case <-b.xong:
			return b.snap, b.err
		case <-ctx.Done():
			return nil, &Loi{Ma: LoiHetGio, ChiTiet: "người gọi huỷ khi đợi hub", Goc: ctx.Err()}
		}
	}
	defer close(b.xong)
	b.snap, b.err = k.tai(ctx, ma, truoc)
	k.mu.Lock()
	if k.dangBay[ma] == b { // đã bị Huy ⇒ không ghi đệm
		delete(k.dangBay, ma)
		if b.err == nil {
			k.nho(b.snap)
		}
	}
	k.mu.Unlock()
	return b.snap, b.err
}

func (k *Khach) tai(ctx context.Context, ma string, truoc *Snapshot) (*Snapshot, error) {
	y := yeuCau{phuongThuc: http.MethodGet, duongDanGiai: "/v1/customers/" + ma + "/entitlements", duongDan: "/v1/customers/" + maHoaThanhPhan(ma) + "/entitlements"}
	if truoc != nil {
		y.ifNoneMatch = truoc.ETag
	}
	res, err := k.gui(ctx, y)
	if err != nil {
		return nil, err
	}
	if res.khongDoi {
		if truoc == nil {
			return nil, &Loi{Ma: LoiPhanHoi, HTTP: 304, ChiTiet: "hub trả 304 nhưng gate không giữ bản nào để gia hạn"}
		}
		moi := *truoc
		if etagHopLe(res.etag) {
			moi.ETag = res.etag // 304 có thể kèm ETag mới
		}
		return &moi, nil
	}
	snap, err := DocSnapshot(res.than, ma, res.etag)
	if err != nil {
		return nil, err
	}
	if snap.TrangThai == TrangLa {
		k.baoLoi(&Loi{Ma: LoiPhanHoi, HTTP: 200, ChiTiet: "trạng thái thuê bao lạ \"" + snap.TrangLaGoc + "\""})
	}
	return snap, nil
}

// nho ghi đệm: có quyền ⇒ theo TTL; KHÔNG có quyền ⇒ tối đa KhongQuyenToiDa. Không bao giờ quá mốc đổi trạng thái hub báo.
func (k *Khach) nho(s *Snapshot) {
	if k.cfg.TTL <= 0 {
		return
	}
	bayGio := k.now()
	ttl := k.cfg.TTL
	if !s.choQuyen() && ttl > KhongQuyenToiDa {
		ttl = KhongQuyenToiDa
	}
	den := bayGio.Add(ttl)
	if m := s.moc(); m != nil && m.Before(den) {
		den = *m
	}
	if den.After(bayGio) {
		k.dem.Dat(s.MaKhach, s, den)
	}
}

// cuDuPhong: bản đệm quá hạn "tươi" nhưng còn trong stale_if_error. Đọc lại SAU lỗi để không dùng bản đã bị Huy. Đọc tươi hỏng
// (`bi ep`): bản đệm coi như đã hết hạn — chỉ stale_if_error > 0 mới cứu bằng bản cũ.
func (k *Khach) cuDuPhong(ma string, biEp bool) *Snapshot {
	m, ok := k.dem.Lay(ma)
	if !ok || (biEp && k.cfg.StaleIfError == 0) {
		return nil
	}
	bayGio := k.now()
	if !bayGio.Before(m.TuoiDen.Add(k.cfg.StaleIfError)) {
		return nil
	}
	// Qua mốc hết kỳ/ân hạn của chính bản này ⇒ hub lỗi là lỗi thật, không "vẫn active từ cache".
	if moc := m.GiaTri.moc(); moc != nil && !bayGio.Before(*moc) {
		return nil
	}
	return m.GiaTri
}
