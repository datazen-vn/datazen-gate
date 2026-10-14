# Triển khai dz-gate

dz-gate là reverse proxy đứng TRƯỚC app có sẵn của bạn: nhận request của khách, nhận diện (JWT / session hook), hỏi hub quyền dùng, rồi cho qua / chuyển tới trang thanh toán / trả 402. Không cần sửa code app. Một tệp chạy được (Go, CGO tắt), image distroless ≈ 9 MB.

```
khách ──TLS──▶ [LB / Caddy / Traefik]  ──HTTP──▶ dz-gate:8080 ──HTTP──▶ app:3000
                                                     │
                                                     └──HTTPS──▶ hub DataZen (api.datazen.vn)
```

## Nhanh (docker compose)

1. Chép `deploy/compose.example.yml` → `compose.yml`, `deploy/gate.example.yaml` → `gate.yaml` (đổi `upstream`, `identity`, `routes`, `paywall`), tạo `.env`:
   ```
   DZ_API_KEY=dz_test_…
   DZ_JWT_SECRET=…        # nếu identity dùng secret_env
   ```
2. `chmod 644 gate.yaml` (container chạy uid 65532, phải đọc được).
3. Build image: `docker build -f deploy/Dockerfile --build-arg PHIEN_BAN=0.1.0 -t dz-gate:0.1.0 .` rồi `docker compose config` (kiểm) và `docker compose up -d`.
4. Kiểm cấu hình trước khi chạy: `docker run --rm --env-file .env -v $PWD/gate.yaml:/c.yaml:ro dz-gate:0.1.0 --check /c.yaml`.
5. `curl -i http://localhost:8080/` đi qua gate.

## Quy tắc BẮT BUỘC

1. **App chỉ được tới được QUA gate.** Upstream tin `X-DZ-*` (khách, gói, tính năng). Gate xoá mọi header giả của client rồi mới tiêm của mình, nhưng nếu app còn mở cổng ra ngoài song song (publish port, cổng LB khác, đường tắt nội bộ), kẻ tấn công gửi thẳng header giả vào app và gate không cứu được. Compose mẫu: `app` KHÔNG `ports:`, mạng `noi-bo` là `internal: true`, chỉ `dz-gate` nằm ở cả `noi-bo` và `ngoai` (cần ra Internet để gọi hub). Bạn tự chịu việc app vẫn kết nối được tới DB của nó (thêm mạng riêng cho DB).
2. **Cổng vận hành (`ops.listen`) bind riêng**, mặc định `127.0.0.1:9090`, không publish. Prometheus ở container khác: đổi sang địa chỉ mạng nội bộ (vd `0.0.0.0:9090` TRONG mạng nội bộ không publish) và scrape qua mạng đó. Không bao giờ để ops ra Internet.
3. **Khoá API trong biến môi trường**, không trong YAML/image/log. Xoay khoá = đổi env rồi khởi động lại (gate đọc khoá lúc khởi động). Khoá bị hub từ chối ⇒ gate từ chối chạy.
4. **TLS kết thúc ở lớp trước gate** (LB/Caddy/Traefik): gate nghe HTTP. Gate đặt `X-Forwarded-Proto` theo `paywall.public_origins` (không tin giá trị client), nên app thấy `https`.
5. **`public_origins` khai đủ mọi domain công khai của app**: là nguồn duy nhất cho `success_url`/`cancel_url` và `X-Forwarded-Host`.

## Header app nhận được

Gate XOÁ khỏi request của client: `X-DZ-*`, `X-Auth-Request-*`, `X-Forwarded-User`, `Forwarded`, `X-Forwarded-*`, `X-Real-IP`, `True-Client-IP`, `CF-Connecting-IP` và các biến thể (xem [cấu hình](cau-hinh.md)); BỎ mọi header có `_` trong tên (trừ khi `inject.allow_underscore_headers`). Rồi đặt `X-Forwarded-For/-Host/-Proto` từ kết nối thật (sau LB, `X-Forwarded-For` là IP của LB: `trusted_proxies` là follow-up) và tiêm `X-DZ-Customer-Ref`, `X-DZ-Entitlement-Status`, `X-DZ-Plan`, `X-DZ-Features`, `X-DZ-Grace-Until`, `X-DZ-Entitlement-Source`, `X-DZ-Gate-Hop: 1`.

`Host` gửi upstream là host của upstream (không phải Host gốc của client): app định tuyến theo vhost cần biết; host gốc đã xác thực nằm ở `X-Forwarded-Host`. Đường dẫn và query được chuyển NGUYÊN VĂN (trừ tham số query không parse được, `ReverseProxy` bỏ). Không có `ReadTimeout` cho thân request (upload/streaming dài): chặn ở lớp trước nếu cần.

## Vận hành

- **Healthcheck** nằm trong image (`/dz-gate --healthcheck`, distroless không có shell). `docker ps` thấy `healthy` khi tiến trình sống. **Readiness**: `GET /_dz/readyz` ở cổng vận hành (upstream TCP + hub/khoá API; 503 khi chưa sẵn sàng; kết quả đệm 5 s).
- **Tắt êm**: SIGTERM/SIGINT ⇒ ngừng nhận kết nối mới, đợi request đang chạy tối đa 10 s, thoát mã 0 (`docker stop -t 15`).
- **Mở rộng ngang**: chạy nhiều bản sau LB được; mỗi bản có bộ đệm riêng (quyền, JWKS, phiên thanh toán) nên tải lên hub ≈ số bản × số khách đang hoạt động / `cache_ttl`. Thu hồi quyền có độ trễ ≤ `cache_ttl` (mặc định 60 s, tối đa 10 phút); chưa có webhook làm mất đệm. Hạn mức khoá API của hub là chung cho mọi bản.
- **Khi hub lỗi**: `fail_mode: closed` (mặc định) ⇒ 503 `dz_hub_unavailable` trên tuyến cần quyền; `stale_if_error` cứu lỗi hạ tầng bằng bản cũ còn trong hạn (không vượt mốc hết kỳ/ân hạn). 401/403/404/429 của hub luôn 503: khoá thu hồi hoặc hết hạn mức không được che.
- **Khi app (upstream) chết**: 502 `dz_upstream_unavailable` sau tối đa 5 s (dial).
- **Quan sát**: `/_dz/metrics` (xem [mã lỗi & số đo](ma-loi.md)); log JSON không chứa cookie/token/khoá/ref thô.
- **Quyền file**: `gate.yaml` đọc được bởi uid 65532; image chạy `read_only`, `cap_drop: ALL`, `no-new-privileges` (compose mẫu).

## Upstream Rails/Spring cũ

`/premium/*` không khớp `/premium.json`, nhưng Rails (`(.:format)`) và Spring < 5.3 định tuyến cả hai về cùng handler. Khai `/premium*` (hoặc cả `/premium` và `/premium.*`), hoặc `default_action: require` rồi liệt kê phần công khai bằng `allow`.

## Kiểm tra gói triển khai

`bash cong/kiem-docker.sh` (cần docker): build image thật, chạy read-only/nonroot, kiểm kích thước < 15 MB, healthcheck, compose mẫu đúng luật (app không publish cổng), request thật qua cổng (200/401/404), cổng vận hành, 502 khi app chết, log không lộ bí mật, tắt êm mã 0.

## Chưa hỗ trợ (follow-up có chủ)

- **Chế độ forward-auth** (`GET /_dz/auth` cho Caddy `forward_auth`/Traefik ForwardAuth/nginx `auth_request`): chưa viết. Dùng chế độ proxy; nếu đã có Caddy/Traefik/nginx, đặt chúng TRƯỚC dz-gate như sơ đồ trên.
- `trusted_proxies` (tin `X-Forwarded-*` của LB đã khai), tuỳ chọn tắt log đường dẫn, publish image (chờ ADR-0002).
