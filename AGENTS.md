# AGENTS.md — gate

> **Bối cảnh toàn hệ: đọc `../meta/AGENTS.md` trước.** Mã ngắn trong kế hoạch: `gate`

> **Không thấy `../meta/`?** Kho này không dùng một mình — nó là một trong các kho ngang hàng
> phải nằm cạnh nhau. Kéo bộ não về rồi để nó dựng lại đủ workspace:
>
> ```bash
> git clone git@github.com:datazen-vn/market-meta.git ../meta
> bash ../meta/workspace/clone-all.sh
> ```

## Vai trò

`dz-gate`: cổng paywall độc lập (Go, một tệp chạy được) đứng trước app có sẵn của dev. Nhận request của khách cuối, nhận diện (JWT / session hook),
hỏi hub quyền dùng (`GET /v1/customers/{ref}/entitlements`), rồi cho qua / `302` tới trang thanh toán hosted / `402` JSON. Cùng hành vi với
`@datazen/sdk/gate` (repo `sdk`): hai mặt tiền, MỘT bộ ca conformance.

## Sở hữu

- `component:dz-gate`

Nguồn sự thật là `../meta/workspace/repos.yaml` — sửa ở đó, không sửa ở đây.

## Tiêu thụ

- `contract:conformance/gate-v1` (`../meta/contracts/conformance/gate-v1/` — bộ ca chung với SDK; đổi hành vi = đổi ca ở meta TRƯỚC)
- `contract:openapi/*` (hình dạng `entitlements`, `checkout-sessions`)

## Bố cục

```
internal/tuyen       khớp đường dẫn: glob first-match, bốn dạng (thô/giải mã/chuẩn hoá/chữ thường), bypass mặc định
internal/danhtinh    resolver danh tính: chuỗi, jwt (+jwks), session_hook, khuôn customer_ref
internal/hub         client hub: entitlements (đệm, ETag/304, stale-if-error, gộp lời gọi, trần đọc tươi), checkout-sessions
internal/quyetdinh   lõi quyết định + paywall (302/401/402/503/508), phiên thanh toán đệm theo replace_after
internal/cauhinh     YAML → struct, kiểm MỌI luật (liệt kê hết), dựng Cong
internal/dem         bộ đệm TTL + FIFO (generic)      internal/gopgoi   singleflight (generic)
cong/                cổng của kho (kiem-kho.sh, kiem-govulncheck.sh)
testdata/            vector JWT/JWKS sinh bằng `jose` của sdk (sinh-vector.mjs) — chứng minh phần xác minh Go khớp cài đặt độc lập
```

Chưa có (PR #2): `internal/proxy`, `internal/vanhanh`, `cmd/dz-gate`, `internal/conformance` (chạy bộ ca meta).

## Lệnh thường dùng

Go 1.26.9 (`go.mod`).

```bash
bash cong/kiem-kho.sh          # gofmt · vet · build · test -race, 0 ca bỏ qua. Mã thoát 0 đạt · 1 trượt · 2 không kiểm được
bash cong/kiem-govulncheck.sh  # lỗ hổng đã biết mà code gọi tới (cần mạng để tải govulncheck)
go test -count=1 -race ./...   # chỉ test
```

Không có CI chạy thật (workspace chốt 13/10): dán đầu ra `cong/kiem-kho.sh` vào PR.

## Ranh giới

- **Không giữ bí mật của dev trong YAML**: khoá API qua `hub.api_key_env`, secret HMAC qua `secret_env`.
- **Không log `customer_ref`/cookie/token/khoá**; lỗi trả ra không chứa chúng (dev hay dùng email làm ref).
- **Fail-closed mặc định.** Chỉ lỗi mạng/timeout/5xx của hub mới được `stale_if_error`/`fail_mode: open` cứu; `401/403/404/429` luôn là `503`.
- Hành vi lệch SDK = một trong hai mặt tiền sai: sửa ca ở meta trước, không "sửa runner cho khớp".

## Cạm bẫy đã biết

**Công khai — semver nghiêm** (khi repo công khai, sau E2E lát 2): chạy trên máy người khác, không ép cập nhật được.
Hai chỗ SDK và gate có thể lệch nếu không canh bằng ca: (1) ngưỡng đệm URL phiên (`replace_after − 30 s`, hub cũ `expires_at − 11 phút`);
(2) trần đọc tươi `checkout_session_id` đặt mốc cả khi đệm trống (README gate-v1 "Trần") — SDK hiện chỉ đặt khi đệm còn tươi.
