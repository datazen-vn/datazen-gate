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
cmd/dz-gate          main mỏng (--config / --check / --healthcheck / --version)
internal/tuyen       khớp đường dẫn: glob first-match, bốn dạng (thô/giải mã/chuẩn hoá/chữ thường), bypass mặc định
internal/danhtinh    resolver danh tính: chuỗi, jwt (+jwks), session_hook, khuôn customer_ref
internal/hub         client hub: entitlements (đệm, ETag/304, stale-if-error, gộp lời gọi, trần đọc tươi), checkout-sessions, Ping
internal/quyetdinh   lõi quyết định + paywall (302/401/402/503/508), phiên thanh toán đệm theo replace_after
internal/proxy       reverse proxy: XOÁ header danh tính/quyền của client, TIÊM X-DZ-*, hop-by-hop, X-Forwarded-* từ kết nối thật, 502 khi upstream chết
internal/vanhanh     log JSON (băm customer_ref), bộ đếm Prometheus, /_dz/{healthz,readyz,metrics} trên bind riêng
internal/cauhinh     YAML → struct, kiểm MỌI luật (liệt kê hết), dựng Cong
internal/chay        ráp tất cả (Lap) + chạy tiến trình + mã thoát
internal/conformance runner Go của bộ ca gate-v1 (meta): proxy thật + hub/hook/upstream giả + đồng hồ giả, bước `parallel`
internal/dem         bộ đệm TTL + FIFO (generic)      internal/gopgoi   singleflight (generic)
cong/                cổng của kho (kiem-kho.sh, kiem-conformance.sh, kiem-govulncheck.sh, hop-dong-ghim.json)
deploy/              gate.example.yaml (= cauhinh.Mau, có test canh), Dockerfile, compose.example.yml
docs/                trien-khai.md, cau-hinh.md, ma-loi.md
testdata/            vector JWT/JWKS sinh bằng `jose` của sdk (sinh-vector.mjs)
```

Chưa có: chế độ `forward-auth`.

## Lệnh thường dùng

Go 1.26.9 (`go.mod`).

```bash
bash cong/kiem-kho.sh          # gofmt · vet · build · test -race, 0 ca bỏ qua (trừ conformance). Mã thoát 0 đạt · 1 trượt · 2 không kiểm được
bash cong/kiem-conformance.sh  # 62 ca gate-v1 của meta qua proxy thật, 0 skip, -race, ghim SHA meta (DZ_META_DIR hoặc ../meta)
bash cong/kiem-docker.sh       # build image thật (distroless nonroot < 15 MB), chạy read-only, request qua cổng, compose mẫu đúng luật (cần docker)
bash cong/kiem-govulncheck.sh  # lỗ hổng đã biết mà code gọi tới (cần mạng để tải govulncheck)
dz-gate --check deploy/gate.example.yaml   # cần DZ_API_KEY trong môi trường
```

Không có CI chạy thật (workspace chốt 13/10): dán đầu ra ba cổng vào PR. Đổi/thêm ca ở meta = bump `cong/hop-dong-ghim.json` (SHA + số ca) CÙNG PR.

## Ranh giới

- **Không giữ bí mật của dev trong YAML**: khoá API qua `hub.api_key_env`, secret HMAC qua `secret_env`.
- **Không log `customer_ref`/cookie/token/khoá**; lỗi trả ra không chứa chúng (dev hay dùng email làm ref).
- **Fail-closed mặc định.** Chỉ lỗi mạng/timeout/5xx của hub mới được `stale_if_error`/`fail_mode: open` cứu; `401/403/404/429` luôn là `503`.
- Hành vi lệch SDK = một trong hai mặt tiền sai: sửa ca ở meta trước, không "sửa runner cho khớp".
- **Danh tính không xác định ⇒ không bao giờ fail-open** (503 + Retry-After). `fail_mode` chỉ áp cho hub entitlements lỗi.
- **Không vòng 409**: khách `active` cùng gói thiếu feature ⇒ 402 không tạo phiên; hỏi lại sau 409 là đọc tươi chịu trần, không xoá đệm quyền (panel an ninh gate#1 C1).
- **Upstream tin `X-DZ-*`**: gate XOÁ mọi `X-DZ-*`, `X-Auth-Request-*`, `X-Forwarded-User` (+ `strip_incoming`) của client rồi mới tiêm. App của dev phải chỉ tới được qua gate (mạng nội bộ compose), nếu không header giả vẫn lọt.

## Cạm bẫy đã biết

**Công khai — semver nghiêm** (khi repo công khai, sau E2E lát 2): chạy trên máy người khác, không ép cập nhật được.
- Đệm URL phiên thanh toán: `replace_after − 30 s` (hub cũ `expires_at − 11 phút`). SDK và gate phải giống nhau (ca `checkout-url-cache-not-past-replace-after`).
- "Đặt mốc trần đọc tươi" và "đăng ký lần gọi hub đang bay" phải nguyên tử (một lần giữ khoá ở `hub.Khach.Lay`); tách ra là race (ca `return-concurrent-*`).
- `ReverseProxy` (Rewrite) bỏ tham số query không parse được (`;`) khỏi request tới upstream; quyết định tuyến chỉ dựa trên đường dẫn, không dựa query.

## Việc để sau (follow-up có chủ)

- `trusted_proxies` (tin `X-Forwarded-*` của LB đã khai) — hiện upstream sau LB thấy IP của LB. Chưa cần tới khi có người triển khai sau LB.
- Tuỳ chọn tắt log đường dẫn (đường dẫn dạng `/invite/<token>` vẫn vào log).
- Các mục THẤP L1–L5 của `reports/panel-261014-1030-gate1-sec.md` (`%00`/`%20` cuối đoạn, `e` JWK dài, cảnh báo `secret` viết thẳng, thông báo lộ upstream, lệch nhỏ SDK/gate khi chạm trần).
