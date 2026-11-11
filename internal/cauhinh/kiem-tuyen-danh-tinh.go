package cauhinh

import (
	"fmt"

	"github.com/datazen-vn/datazen-gate/internal/tuyen"
)

func coFailMoCua(c *Config) bool {
	if c.Hub.FailMode == "open" {
		return true
	}
	for _, r := range c.Routes {
		if r.FailMode == "open" && r.Action != tuyen.Cho {
			return true
		}
	}
	return false
}

func kiemTuyen(k *KetQuaKiem, c *Config) {
	if n := len(c.Routes); n < 1 || n > soTuyenToiDa {
		k.loi("routes", "phải có 1–%d dòng, có %d", soTuyenToiDa, len(c.Routes))
	}
	if c.DefaultAction != tuyen.Cho && c.DefaultAction != tuyen.Can {
		k.loi("default_action", "phải là allow hoặc require, nhận %q", c.DefaultAction)
	}
	for i, r := range c.Routes {
		t := fmt.Sprintf("routes[%d]", i)
		if !matchRe.MatchString(r.Match) {
			k.loi(t+".match", "phải bắt đầu bằng \"/\" (glob, ASCII in được, ≤ 201 ký tự), nhận %q", r.Match)
		}
		switch r.Action {
		case tuyen.Cho, tuyen.Can, tuyen.Mem:
		default:
			k.loi(t+".action", "phải là allow, require hoặc soft, nhận %q", r.Action)
		}
		if r.Feature != "" && !featureRe.MatchString(r.Feature) {
			k.loi(t+".feature", "không được chứa khoảng trắng, dấu phẩy hay ký tự điều khiển (≤ 128 ký tự)")
		}
		if r.Feature != "" && r.Action == tuyen.Cho {
			k.canh(t+".feature", "tuyến allow không kiểm quyền nên feature bị bỏ qua")
		}
		if r.PlanCode != "" && !maGoiRe.MatchString(r.PlanCode) {
			k.loi(t+".plan_code", "phải khớp ^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$")
		}
		if r.FailMode != "" && r.FailMode != "open" && r.FailMode != "closed" {
			k.loi(t+".fail_mode", "phải là open hoặc closed, nhận %q", r.FailMode)
		}
	}
	if c.Paywall.SuccessPath != "" {
		bang := tuyen.Dong{}
		for _, r := range c.Routes {
			if r.Action == tuyen.Can && tuyen.KhopGlob(r.Match, c.Paywall.SuccessPath) {
				bang = tuyen.Dong{Khop: r.Match}
				break
			}
		}
		if bang.Khop != "" {
			k.canh("paywall.success_path", "khớp tuyến require %q: success_path luôn là bypass (không bị chặn) nên tuyến đó không có tác dụng ở đây, nhưng thường là dấu hiệu cấu hình nhầm", bang.Khop)
		}
	}
}

func kiemDanhTinh(k *KetQuaKiem, c *Config, env Env) {
	if n := len(c.Identity); n < 1 || n > soDanhTinhToiDa {
		k.loi("identity", "phải có 1–%d resolver, có %d", soDanhTinhToiDa, n)
	}
	for i, id := range c.Identity {
		t := fmt.Sprintf("identity[%d]", i)
		if _, err := id.Bo(env, nil, nil); err != nil {
			k.loi(t, "%v", err)
			continue
		}
		if id.Secret != "" {
			k.canh(t+".secret", "secret viết thẳng trong YAML: mọi người đọc được tệp cấu hình/bản sao lưu/git đều thấy; dùng secret_env (biến môi trường) và chỉ để secret viết thẳng khi thử")
		}
		if id.Type == "jwt" && (id.Issuer == "" || id.Audience == "") {
			k.canh(t, "jwt không đặt issuer/audience: token của mọi bên cùng ký bằng khoá này đều qua")
		}
	}
}
