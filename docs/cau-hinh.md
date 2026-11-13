# Cấu hình dz-gate

Một tệp YAML (hoặc JSON, tập con của YAML). **Trường lạ là lỗi** (gõ sai `fail_mod` không âm thầm bị bỏ qua), đúng MỘT tài liệu, ≤ 1 MB. Bí mật không nằm trong tệp: khoá API qua `hub.api_key_env`, secret HMAC qua `identity[].secret_env`.

```bash
dz-gate --check gate.yaml                 # liệt kê MỌI lỗi/cảnh báo một lượt rồi thoát (0 hợp lệ · 1 sai · 2 không đọc được tệp)
dz-gate --config gate.yaml                # chạy
dz-gate --healthcheck --config gate.yaml  # GET /_dz/healthz của tiến trình đang chạy (cho HEALTHCHECK container)
dz-gate --version
```

`--check` đọc biến môi trường thật (cần `DZ_API_KEY`). Mẫu đầy đủ, có chú thích: [`deploy/gate.example.yaml`](../deploy/gate.example.yaml) (một nguồn duy nhất với `internal/cauhinh/mau.go`, có test canh).

Tệp gắn vào container phải **đọc được bởi uid 65532** (nonroot) và chỉ bởi uid đó: `sudo chown 65532:65532 gate.yaml && chmod 0400 gate.yaml`.

## `gate`

| Trường | Mặc định | Luật |
|---|---|---|
| `listen` | (bắt buộc) | `host:port` hoặc `:port`, cổng 1–65535 |
| `upstream` | (bắt buộc) | URL `http(s)://host[:port]` của app, không `user:password`; **không** được trỏ về chính `listen` (gate tự proxy chính nó) |
| `mode` | `proxy` | chỉ `proxy` (forward-auth: chưa hỗ trợ, xem [triển khai](trien-khai.md)) |
| `trusted_proxies` | `[]` | CIDR/IP đơn (IPv4/IPv6) của LB/CDN đứng ngay trước gate. Rỗng = không tin ai (IP khách = kết nối trực tiếp). Có ⇒ chỉ kết nối từ các dải này được tin `X-Forwarded-For` (phải→trái, bỏ hop tin cậy, IP không tin đầu tiên = khách; gate đặt lại header = IP đó) và `X-Forwarded-Proto` (`http`/`https`). Hỏng/quá 1 KiB ⇒ dùng IP kết nối. Token dạng `ip:port`/`[v6]:port` được nhận (bỏ cổng). **Lưu ý:** XFF dài >1 KiB hoặc có token rác ⇒ app thấy IP của LB, nên app KHÔNG được coi IP nội bộ của LB là tin cậy (allowlist admin, miễn giới hạn tốc độ). Topo CDN→LB→gate: `X-Forwarded-Proto` là của chặng CDN→LB (thường `http`) và đè scheme của `public_origins` — app ép HTTPS có thể lặp redirect; khi đó đừng khai CDN/LB đó, hoặc cấu hình LB đặt Proto theo scheme của khách. `--check`: sai CIDR là lỗi; `0.0.0.0/0`/`::/0` cảnh báo (ai cũng giả được IP) |

## `hub`

| Trường | Mặc định | Luật |
|---|---|---|
| `base_url` | `https://api.datazen.vn` | https; `http` chỉ cho `localhost:<cổng>`/`127.0.0.1:<cổng>` (có cổng); không `user:password` |
| `api_key_env` | `DZ_API_KEY` | tên biến môi trường; giá trị phải bắt đầu `dz_test_` hoặc `dz_live_`. Thay vào đó có thể đặt `<tên>_FILE` (mặc định `DZ_API_KEY_FILE`) = đường dẫn tệp chứa khoá (Docker/Kubernetes secrets, không hiện ra ở `docker inspect`): tệp thường ≤ 4 KiB, xuống dòng cuối được cắt. Đặt CẢ HAI là lỗi. Quyền rộng hơn 0600 (nhóm/người khác đọc được) là cảnh báo, đặt 0400 |
| `cache_ttl` | `60s` | `0s` = tắt đệm; tối đa `10m`. Quyền KHÔNG có (none/expired/cancelled/lạ) chỉ đệm tối đa 15 s dù TTL dài hơn |
| `stale_if_error` | `0s` (tắt) | tối đa `1h`; cần `cache_ttl > 0`; chỉ cứu lỗi mạng/timeout/5xx; không bao giờ vượt `current_period_end`/`grace_until` của bản đệm |
| `fail_mode` | `closed` | `closed`: hub lỗi ⇒ 503. `open`: cho qua (header `X-DZ-Entitlement-Source: fail-open`), chỉ với lỗi mạng/timeout/5xx. **401/403/404/429 của hub luôn 503**, kể cả `open` |
| `timeout` | `5s` | (0, 60s] |

Cảnh báo: tệp khoá quyền rộng hơn 0600; khoá `dz_live_` + `fail_mode: open` (toàn cục hoặc tuyến): hub không trả lời thì tuyến trả phí mở cửa miễn phí.

## `identity` (1–8 resolver, thử lần lượt)

Resolver đầu nhận ra khách ⇒ dừng. Token sai/hết hạn = "không nhận ra ai" (thử tiếp); lỗi hạ tầng (JWKS/hook không trả lời) = **không biết là ai** ⇒ 503 + `Retry-After: 5`, **không bao giờ** cho qua ẩn danh trên tuyến cần quyền, kể cả `fail_mode: open`. `customer_ref` giải ra phải 1–200 ký tự ASCII in được không khoảng trắng (trừ `.`/`..`), sai ⇒ coi như chưa đăng nhập.

`type: jwt` — app đã phát JWT; gate xác minh rồi lấy claim làm `customer_ref`:

| Trường | Mặc định | Luật |
|---|---|---|
| `jwks_url` **hoặc** `secret`/`secret_env` | — | đúng MỘT. `jwks_url`: https (hoặc loopback có cổng). `secret` ≥ 32 byte, HS*; **ưu tiên `secret_env`** (`secret` viết thẳng chỉ để thử) |
| `secret` viết thẳng | — | cảnh báo: ai đọc được tệp cấu hình/bản sao lưu/git đều thấy; dùng `secret_env` |
| `issuer`, `audience` | (không) | NÊN đặt: không đặt thì token của mọi bên cùng ký bằng khoá này đều qua (cảnh báo) |
| `subject_claim` | `sub` | `[A-Za-z0-9_.-]{1,64}`; giá trị chuỗi hoặc số nguyên an toàn |
| `algorithms` | HS256 (secret) · RS256+ES256 (JWKS) | danh sách ĐÓNG tách theo loại khoá: secret chỉ HS*, JWKS chỉ RS*/PS*/ES*/EdDSA; không `none` |
| `from` | `authorization` | `authorization` (Bearer) · `cookie:<tên>` · `header:<tên>` |
| `clock_tolerance` | `5s` | 0–60 s; `exp` bắt buộc |
| `timeout` | `5s` | tải JWKS |

JWKS được đệm (làm mới sau 10 phút hoặc khi gặp `kid` lạ, tối đa 1 lần / 30 s); tải lỗi ⇒ đệm âm 30 s + jitter (không tải lại mỗi request). RSA ≥ 2048 bit.

`type: session_hook` — hỏi chính app "người này là ai" (cookie opaque):

| Trường | Mặc định | Luật |
|---|---|---|
| `url` | (bắt buộc) | 200 + JSON khi đã đăng nhập, 401/403 khi chưa. **Chỉ loopback hoặc host trong `allow_hosts`** (chống SSRF/lộ cookie); không `user:password`; không theo chuyển hướng |
| `allow_hosts` | (không) | host[:port] ngoài loopback được nhận cookie, khớp nguyên tên (vd `app:3000`) |
| `forward` | `cookie, authorization` | 1–8 tên header chuyển tiếp; không có header nào ⇒ chưa đăng nhập, KHÔNG gọi hook |
| `subject_path` | `user.id` | ≤ 5 đoạn `[A-Za-z0-9_]`, không `__proto__`/`constructor`/`prototype` |
| `timeout` | `2s` | (0, 10s] |
| `cache_ttl` | `30s` | `0s` = tắt; tối đa `10m`; chỉ đệm "đã đăng nhập", khoá bằng SHA-256 của header chuyển tiếp |

## `routes` (1–128 dòng, first-match)

`{ match, action, feature?, plan_code?, fail_mode? }`

- `match`: glob `*` (khớp mọi chuỗi kể cả `/`), bắt đầu bằng `/`, ≤ 201 ký tự; so nguyên văn, hoa/thường do dạng đường dẫn riêng lo; `/x/*` khớp cả `/x`. Mỗi đường dẫn được xét ở 4 dạng (thô, giải mã ≤ 3 vòng, chuẩn hoá, chữ thường): `require` thắng khi MỘT dạng khớp.
- `action`: `allow` (không hỏi hub, không cần danh tính) · `require` (cần thuê bao còn hiệu lực; chưa đăng nhập ⇒ 401/302 login) · `soft` (không bao giờ chặn, vẫn tiêm quyền nếu có danh tính).
- `feature`: khoá trong `features` của gói (không khoảng trắng/dấu phẩy). Thiếu ⇒ 402. Khách `active` đúng gói mà thiếu feature ⇒ 402 KHÔNG tạo phiên thanh toán.
- `plan_code`: gói bán khi chặn tuyến này (mặc định `paywall.plan_code`); tuyến của gói cao hơn phải khai để khách đổi gói được.
- `fail_mode`: ghi đè `hub.fail_mode` cho tuyến.

`default_action` (`allow` mặc định): đường dẫn không khớp dòng nào ⇒ cho qua không hỏi hub; `require` ⇒ coi như tuyến `require` không feature. **Bypass luôn có** (xét trước `routes`): `/api/dz/*`, `/_dz/*`, `login_url`, `success_path`. `/_dz/*` ở cổng công khai do gate tự trả 404.

Upstream Rails/Spring cũ: `/premium/*` KHÔNG khớp `/premium.json`; khai `/premium*` hoặc dùng `default_action: require`.

## `paywall`

| Trường | Luật |
|---|---|
| `plan_code` | `^[a-z0-9]([a-z0-9-]{0,62}[a-z0-9])?$` |
| `public_origins` | 1–8 origin `https://host[:port]` (http chỉ loopback có cổng) của chính app; **nguồn duy nhất** cho `success_url`/`cancel_url` và `X-Forwarded-Host`/`-Proto`. App nhiều domain phải khai đủ |
| `login_url` | đường dẫn `/login` hoặc URL tuyệt đối; luôn bypass nếu cùng host với một origin |
| `success_path` | nơi hub đưa khách về sau khi trả; luôn bypass; khai ⇒ gate làm tươi quyền khi có `?checkout_session_id=<uuid>`. Chỉ là đường dẫn (không `?`/`#`) |
| `cancel_path` | mặc định `/`; không `?`/`#` |

## `inject`

- `strip_incoming`: tên header (chữ thường, glob `*`) cần xoá thêm khỏi request đến. Ví dụ app có hỗ trợ ghi đè phương thức (`X-HTTP-Method-Override`, `X-HTTP-Method`, `X-Method-Override`) mà không dùng: thêm vào đây (mặc định gate không xoá vì có thể làm vỡ API hợp lệ).
- `allow_underscore_headers` (`false`): mặc định gate BỎ mọi header có `_` trong tên của client. Bật chỉ khi app thật sự cần (có cảnh báo).
- Luôn xoá (không cấu hình được): `x-dz-*`, `x-auth-request-*`, `x-forwarded-user`, `forwarded`, `x-forwarded-*`, `x-original-forwarded-*`, `x-real-ip`, `x-client-ip`, `x-cluster-client-ip`, `true-client-ip`, `cf-connecting-ip`, `fastly-client-ip`, `fly-client-ip`, `client-ip`, `x-originating-ip`, `x-remote-ip`, `x-remote-addr`; header ghi đè đường dẫn/host `x-original-url`, `x-rewrite-url`, `x-original-uri`, `x-original-host`, `x-host`, `x-http-host-override`. Tên được chuẩn hoá (`_` → `-`, chữ thường) trước khi so.

Header gate tiêm cho upstream: `X-DZ-Customer-Ref`, `X-DZ-Entitlement-Status`, `X-DZ-Plan`, `X-DZ-Features` (csv, sắp xếp), `X-DZ-Grace-Until` (RFC 3339 UTC, chỉ khi đang ân hạn), `X-DZ-Entitlement-Source` (`network|cache|stale|fail-open`), `X-DZ-Gate-Hop: 1`. Chỉ có khi cho qua và có khách; bypass ẩn danh chỉ có `Gate-Hop`.

## `ops`

| Trường | Mặc định | Luật |
|---|---|---|
| `listen` | `127.0.0.1:9090` | bind RIÊNG, ≠ `gate.listen`. Trong docker giữ `127.0.0.1:9090`; không bao giờ `0.0.0.0` (bind cả mạng nội bộ lẫn mạng ngoài). Không phải loopback ⇒ `--check` cảnh báo (không lỗi) |
| `log_customer_ref` | `hash` | `hash` (`sha256(salt‖ref)[:16]`, salt ngẫu nhiên mỗi tiến trình) hoặc `none`; không bao giờ log ref thô |

Cổng vận hành: `/_dz/healthz` (tiến trình sống), `/_dz/readyz` (upstream TCP + hub/khoá API, đệm 5 s, 503 khi chưa sẵn sàng), `/_dz/metrics` (Prometheus).
