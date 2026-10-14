package conformance

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
)

// kiemPhanHoi: response.status/headers/json (tập con)/json_absent của phản hồi NGƯỜI GỌI thấy (do gate trả, hoặc của upstream khi cho qua).
func (r *chayCa) kiemPhanHoi(where string, rec *httptest.ResponseRecorder, want *PhanHoiKy) {
	if want == nil {
		return
	}
	if rec.Code != want.Status {
		r.t.Errorf("%s: response.status = %d, mong %d", where, rec.Code, want.Status)
	}
	for _, l := range soHeader(rec.Header(), want.Headers, where+" response") {
		r.t.Error(l)
	}
	if want.JSON != nil || len(want.JSONAbsent) > 0 {
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			r.t.Errorf("%s: thân không phải JSON: %q", where, rec.Body.String())
			return
		}
		if want.JSON != nil && !tapCon(got, want.JSON) {
			r.t.Errorf("%s: response.json = %v, mong tập con %v", where, got, want.JSON)
		}
		for _, k := range want.JSONAbsent {
			if _, co := got[k]; co {
				r.t.Errorf("%s: response.json không được có khoá %q", where, k)
			}
		}
	}
}

func (r *chayCa) kiemSoGoi(where string, from int, want *SoGoi) {
	if want == nil {
		return
	}
	for loai, n := range map[string]*int{"entitlements": want.Entitlements, "checkout": want.Checkout} {
		if n == nil {
			continue
		}
		if got := len(r.hub.gocTu(from, loai)); got != *n {
			r.t.Errorf("%s: hub_calls.%s = %d, mong %d", where, loai, got, *n)
		}
	}
}

// chayMot: một request tuần tự + mọi kỳ vọng.
func (r *chayCa) chayMot(where string, y YeuCau, e Expect) {
	from := r.hub.soGoi()
	r.up.begin()
	rec, req := r.gui(y)
	r.mu.Lock()
	kq, co := r.quyet[req]
	r.mu.Unlock()
	if !co {
		r.t.Fatalf("%s: không lấy được quyết định của gate", where)
	}
	for _, l := range soQuyet(kq.Quyet, e.Decision) {
		r.t.Errorf("%s: %s", where, l)
	}
	r.kiemPhanHoi(where, rec, e.Response)
	r.up.mu.Lock()
	called, hdr := r.up.called, r.up.header
	r.up.mu.Unlock()
	if e.UpstreamCalled != nil && called != *e.UpstreamCalled {
		r.t.Errorf("%s: upstream_called = %v, mong %v", where, called, *e.UpstreamCalled)
	}
	if e.UpstreamCalledTheoMatTien != nil && e.UpstreamCalledTheoMatTien.Gate != nil && called != *e.UpstreamCalledTheoMatTien.Gate {
		r.t.Errorf("%s: upstream_called (mặt tiền gate) = %v, mong %v", where, called, *e.UpstreamCalledTheoMatTien.Gate)
	}
	if e.UpstreamRequestHeaders != nil {
		if !called {
			r.t.Errorf("%s: upstream phải được gọi để kiểm header", where)
		} else {
			for _, l := range soHeader(hdr, e.UpstreamRequestHeaders, where+" upstream") {
				r.t.Error(l)
			}
		}
	}
	r.kiemSoGoi(where, from, e.HubCalls)
	if e.HubRequestHeaders != nil {
		ds := r.hub.gocTu(from, "entitlements")
		if len(ds) == 0 {
			r.t.Errorf("%s: có lần gọi entitlements để kiểm header", where)
		} else {
			for _, l := range soHeader(ds[len(ds)-1].header, e.HubRequestHeaders, where+" hub") {
				r.t.Error(l)
			}
		}
	}
	if e.CheckoutRequest != nil {
		ds := r.hub.gocTu(from, "checkout")
		if len(ds) == 0 {
			r.t.Errorf("%s: có lần gọi checkout để kiểm thân", where)
		} else if !tapCon(ds[len(ds)-1].than, e.CheckoutRequest) {
			r.t.Errorf("%s: checkout_request = %v, mong tập con %v", where, ds[len(ds)-1].than, e.CheckoutRequest)
		}
	}
}

// chaySong: bước `parallel` — gửi TẤT CẢ cùng lúc qua MỘT gate (cùng bộ đệm) rồi đợi xong mới kiểm; hub_calls đếm cho cả bước.
func (r *chayCa) chaySong(where string, s Song) {
	from := r.hub.soGoi()
	type ketQua struct {
		rec *httptest.ResponseRecorder
		req *http.Request
	}
	kq := make([]ketQua, len(s.Requests))
	var wg sync.WaitGroup
	batDau := make(chan struct{})
	for i := range s.Requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-batDau
			rec, req := r.gui(s.Requests[i].Request)
			kq[i] = ketQua{rec, req}
		}()
	}
	close(batDau)
	wg.Wait()
	for i, k := range kq {
		w := where + " request " + itoa(i+1)
		r.mu.Lock()
		q, co := r.quyet[k.req]
		r.mu.Unlock()
		if !co {
			r.t.Fatalf("%s: không lấy được quyết định của gate", w)
		}
		for _, l := range soQuyet(q.Quyet, s.Requests[i].Expect.Decision) {
			r.t.Errorf("%s: %s", w, l)
		}
		r.kiemPhanHoi(w, k.rec, s.Requests[i].Expect.Response)
	}
	r.kiemSoGoi(where, from, s.HubCalls)
}
