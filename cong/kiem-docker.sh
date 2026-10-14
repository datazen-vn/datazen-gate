#!/usr/bin/env bash
# Cong dong goi: build image that, chay DUNG cach trien khai (khong shell, nonroot, read-only), va di mot vong qua cong + cong van hanh.
#
#   bash cong/kiem-docker.sh
#
# MA THOAT: 0 dat · 1 TRUOT (image > 15 MB, khong healthy, hanh vi sai, ro ri bi mat, compose mau sai luat) · 2 KHONG KIEM DUOC (khong co docker,
# daemon khong tra loi, khong keo duoc image can thiet).
#
# Kiem (moi muc la mot dong `[DAT]`/`[DO]`):
#   image < 15 MB · USER nonroot · --version · --check deploy/gate.example.yaml · compose mau: `app` KHONG publish cong, chi `dz-gate` publish 8080 TREN
#   LOOPBACK, co mem_limit/pids_limit, mang noi-bo internal · moi `FROM` cua Dockerfile ghim digest · healthcheck cua image tu `starting` len `healthy` · app KHONG publish cong · cong van hanh KHONG publish ·
#   request qua cong: tuyen allow toi app (200), tuyen require khong danh tinh 401 no-store, /_dz/* mat cong khai 404 · cong van hanh (netns chung):
#   healthz, metrics · app chet => 502 dz_upstream_unavailable · log KHONG chua khoa API/secret · docker stop => thoat ma 0.
#
# Tai nguyen dat ten `dzgate-kiem-<pid>-*` va CHI nhung cai do bi don (may dung chung voi cac phien khac).
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
command -v docker >/dev/null 2>&1 || { echo "[MÃ 2] KHÔNG KIỂM ĐƯỢC — không có docker"; exit 2; }
docker info >/dev/null 2>&1 || { echo "[MÃ 2] KHÔNG KIỂM ĐƯỢC — daemon docker không trả lời"; exit 2; }

ID="dzgate-kiem-$$"
NET="$ID-net"; IMG="$ID:kiem"; APP="$ID-app"; GATE="$ID-gate"
BUSYBOX="busybox:1.36"; CURL="curlimages/curl:8.11.1"
TMP="$(mktemp -d)" || exit 2
KHOA="dz_test_kiem-khoa-bi-mat-123456"; BI_MAT_JWT="kiem-secret-jwt-bi-mat-0123456789abcdef"
FAIL=0

cleanup() {
  docker rm -f "$GATE" "$APP" >/dev/null 2>&1
  docker network rm "$NET" >/dev/null 2>&1
  docker rmi -f "$IMG" >/dev/null 2>&1
  rm -rf "$TMP"
}
trap cleanup EXIT

# KHÔNG `echo "$R" | grep -q`: với `set -o pipefail`, grep -q thoát sớm làm echo SIGPIPE ⇒ pipeline 141 ⇒ báo ĐỎ giả. Dùng here-string.
dong1() { head -1 <<<"$1"; }
co() { grep -q -e "$2" <<<"$1"; }
cokhong() { grep -qi -e "$2" <<<"$1"; }
dat() { echo "[ĐẠT] $1"; }
do_() { echo "[ĐỎ] $1"; FAIL=1; }
kt() { # kt "mo ta" <lenh tra 0 khi dung>
  local mo="$1"; shift
  if "$@" >/dev/null 2>&1; then dat "$mo"; else do_ "$mo"; fi
}

for anh in "$BUSYBOX" "$CURL"; do
  docker image inspect "$anh" >/dev/null 2>&1 || docker pull -q "$anh" >/dev/null 2>&1 \
    || { echo "[MÃ 2] KHÔNG KIỂM ĐƯỢC — không có/không kéo được image $anh"; exit 2; }
done

echo "── build ──"
docker build -q -f deploy/Dockerfile --build-arg PHIEN_BAN=kiem -t "$IMG" . >/dev/null || { echo "[ĐỎ] docker build"; exit 1; }
KICH_THUOC=$(docker image inspect -f '{{.Size}}' "$IMG")
if [ "$KICH_THUOC" -lt $((15 * 1024 * 1024)) ]; then dat "image $((KICH_THUOC / 1024 / 1024)) MB < 15 MB"; else do_ "image $((KICH_THUOC / 1024 / 1024)) MB ≥ 15 MB"; fi
[ "$(docker image inspect -f '{{.Config.User}}' "$IMG")" = "nonroot:nonroot" ] && dat "USER nonroot:nonroot" || do_ "image không chạy nonroot"
[ "$(docker run --rm "$IMG" --version)" = "dz-gate kiem" ] && dat "--version" || do_ "--version"
# Tệp cấu hình gắn vào container phải ĐỌC ĐƯỢC bởi uid 65532 (nonroot): chép ra tmp rồi chmod 644 (tệp 0600 trong kho sẽ không đọc được).
cp deploy/gate.example.yaml "$TMP/mau.yaml" && chmod 644 "$TMP/mau.yaml"
kt "--check deploy/gate.example.yaml" docker run --rm -e DZ_API_KEY="$KHOA" -v "$TMP/mau.yaml:/c.yaml:ro" "$IMG" --check /c.yaml

echo "── compose mẫu ──"
mkdir -p "$TMP/cp" && cp deploy/compose.example.yml "$TMP/cp/compose.yml" && cp deploy/gate.example.yaml "$TMP/cp/gate.yaml" && echo "DZ_API_KEY=$KHOA" >"$TMP/cp/.env"
if docker compose -f "$TMP/cp/compose.yml" config --format json >"$TMP/cp/cfg.json" 2>"$TMP/cp/err.txt"; then
  if command -v python3 >/dev/null 2>&1; then
    python3 - "$TMP/cp/cfg.json" <<'PY' && dat "compose mẫu: app không publish cổng, dz-gate chỉ publish 8080 trên loopback, noi-bo internal" || do_ "compose mẫu sai luật (app có ports / publish thừa hoặc ra 0.0.0.0 / noi-bo không internal)"
import json, sys
c = json.load(open(sys.argv[1]))
s = c["services"]
assert not s["app"].get("ports"), "app không được publish cổng"
pub = [(p.get("published"), p.get("target")) for p in s["dz-gate"].get("ports", [])]
assert pub == [("8080", 8080)], f"dz-gate chỉ publish 8080: {pub}"
# Gate nghe HTTP trơn: publish ra 0.0.0.0 (mặc định khi thiếu host_ip) vượt ufw/firewalld và phơi cookie/Bearer ra Internet.
ips = [p.get("host_ip") for p in s["dz-gate"]["ports"]]
assert all(i in ("127.0.0.1", "::1") for i in ips), f"cổng publish phải bind loopback, đang: {ips}"
assert c["networks"]["noi-bo"].get("internal") is True, "noi-bo phải internal"
assert "noi-bo" in s["app"]["networks"] and "ngoai" not in s["app"]["networks"], "app chỉ ở noi-bo"
assert set(s["dz-gate"]["networks"]) == {"noi-bo", "ngoai"}
assert s["dz-gate"].get("mem_limit") and s["dz-gate"].get("pids_limit"), "dz-gate phải có mem_limit và pids_limit"
PY
  else
    echo "[MÃ 2] KHÔNG KIỂM ĐƯỢC compose mẫu — không có python3"; exit 2
  fi
else
  do_ "docker compose config: $(head -c 200 "$TMP/cp/err.txt")"
fi

# Base image ghim theo digest: tag bị đẩy đè thì build không đổi lặng lẽ (chuỗi cung ứng, build tái lập).
if grep -E '^FROM ' deploy/Dockerfile | grep -vqE '@sha256:[0-9a-f]{64}'; then do_ "deploy/Dockerfile có FROM chưa ghim digest"; else dat "mọi FROM của Dockerfile ghim digest"; fi

echo "── chạy ──"
docker network create "$NET" >/dev/null || { echo "[MÃ 2] KHÔNG KIỂM ĐƯỢC — không tạo được mạng docker"; exit 2; }
docker run -d --name "$APP" --network "$NET" "$BUSYBOX" sh -c 'mkdir -p /w && echo app-ok >/w/index.html && exec httpd -f -p 3000 -h /w' >/dev/null || { echo "[MÃ 2] không chạy được app giả"; exit 2; }
cat >"$TMP/gate.yaml" <<YAML
gate: { listen: ":8080", upstream: "http://$APP:3000" }
hub: { base_url: "http://127.0.0.1:1", cache_ttl: 30s }
identity:
  - { type: jwt, secret_env: DZ_JWT_SECRET, issuer: kiem, audience: kiem }
routes:
  - { match: "/api/public/*", action: allow }
  - { match: "/dashboard/*", action: require, feature: dashboard }
paywall: { plan_code: pro, public_origins: ["https://app.kiem.test"] }
ops: { listen: "127.0.0.1:9090" }
YAML
chmod 644 "$TMP/gate.yaml"
docker run -d --name "$GATE" --network "$NET" -v "$TMP/gate.yaml:/etc/dz-gate/gate.yaml:ro" \
  -e DZ_API_KEY="$KHOA" -e DZ_JWT_SECRET="$BI_MAT_JWT" \
  --read-only --cap-drop ALL --security-opt no-new-privileges:true "$IMG" >/dev/null || { do_ "không chạy được gate (read-only/cap-drop/nonroot)"; exit 1; }

TRANG_THAI=""
for _ in $(seq 1 40); do
  TRANG_THAI=$(docker inspect -f '{{.State.Health.Status}}' "$GATE" 2>/dev/null)
  [ "$TRANG_THAI" = "healthy" ] && break
  [ "$(docker inspect -f '{{.State.Running}}' "$GATE" 2>/dev/null)" = "true" ] || break
  sleep 1
done
if [ "$TRANG_THAI" = "healthy" ]; then dat "healthcheck của image: healthy"; else do_ "healthcheck = '$TRANG_THAI'"; docker logs "$GATE" 2>&1 | tail -5; fi

[ -z "$(docker port "$APP")" ] && dat "app KHÔNG publish cổng" || do_ "app publish cổng"
[ -z "$(docker port "$GATE")" ] && dat "gate (chạy không -p) không publish cổng nào, kể cả vận hành 9090" || do_ "gate publish cổng ngoài ý muốn"

lay() { docker run --rm --network "$NET" "$CURL" -s -i -m 5 "$@"; }
layops() { docker run --rm --network "container:$GATE" "$CURL" -s -i -m 5 "$@"; }

R=$(lay "http://$GATE:8080/index.html")
co "$(dong1 "$R")" " 200" && co "$R" "app-ok" && dat "tuyến không khớp (mặc định allow) tới app: 200" || do_ "tuyến mặc định không tới app: $(dong1 "$R")"
R=$(lay -H "Accept: application/json" "http://$GATE:8080/dashboard/x")
{ co "$(dong1 "$R")" " 401"; } && cokhong "$R" "^cache-control: no-store" && co "$R" "dz_no_identity" && dat "tuyến require không danh tính: 401 no-store dz_no_identity" || do_ "require không danh tính: $(dong1 "$R")"
R=$(lay -H "Accept: text/html" -H "Sec-Fetch-Mode: navigate" "http://$GATE:8080/dashboard/x")
co "$(dong1 "$R")" " 401" && dat "không login_url ⇒ trình duyệt cũng 401 (không có nơi để 302)" || do_ "điều hướng không danh tính: $(dong1 "$R")"
R=$(lay "http://$GATE:8080/_dz/healthz")
co "$(dong1 "$R")" " 404" && co "$R" "dz_not_found" && dat "/_dz/* ở mặt công khai: gate tự trả 404" || do_ "/_dz/* lộ ra mặt công khai: $(dong1 "$R")"
R=$(lay -H "X-DZ-Customer-Ref: admin" -H "X_DZ_Customer_Ref: admin" "http://$GATE:8080/index.html")
co "$(dong1 "$R")" " 200" && dat "header giả không làm hỏng request (đã được kiểm kỹ ở test proxy)" || do_ "request có header giả: $(dong1 "$R")"

R=$(layops "http://127.0.0.1:9090/_dz/healthz")
co "$(dong1 "$R")" " 200" && dat "cổng vận hành (netns chung) /_dz/healthz 200" || do_ "ops healthz: $(dong1 "$R")"
R=$(layops "http://127.0.0.1:9090/_dz/metrics")
co "$R" 'dz_gate_requests_total{ket_qua="no_identity"}' && dat "/_dz/metrics có đếm request" || do_ "ops metrics thiếu số đo"
R=$(lay "http://$GATE:9090/_dz/healthz" -m 3)
co "$(dong1 "$R")" "HTTP" && do_ "cổng vận hành nghe được từ mạng ngoài netns (ops.listen=127.0.0.1 phải chỉ loopback)" || dat "cổng vận hành KHÔNG tới được từ container khác"

docker stop "$APP" >/dev/null 2>&1
# Dial tới app chết hết hạn sau 5 s (DialContext.Timeout) nên chờ lâu hơn mặc định.
R=$(lay -m 20 "http://$GATE:8080/index.html")
co "$(dong1 "$R")" " 502" && co "$R" "dz_upstream_unavailable" && ! co "$R" "$APP" && dat "app chết ⇒ 502 dz_upstream_unavailable, không lộ địa chỉ" || do_ "app chết: $(dong1 "$R")"

LOG=$(docker logs "$GATE" 2>&1)
if co "$LOG" "$KHOA" || co "$LOG" "$BI_MAT_JWT"; then do_ "log chứa khoá API/secret"; else dat "log không chứa khoá API/secret"; fi

docker stop -t 15 "$GATE" >/dev/null 2>&1
[ "$(docker inspect -f '{{.State.ExitCode}}' "$GATE" 2>/dev/null)" = "0" ] && dat "docker stop ⇒ tắt êm, mã thoát 0" || do_ "mã thoát sau SIGTERM = $(docker inspect -f '{{.State.ExitCode}}' "$GATE" 2>/dev/null)"

if [ "$FAIL" -ne 0 ]; then echo "[ĐỎ] cổng đóng gói trượt"; exit 1; fi
echo "[XANH] đóng gói: image $((KICH_THUOC / 1024 / 1024)) MB, nonroot, read-only, healthy, mọi kiểm đạt."
