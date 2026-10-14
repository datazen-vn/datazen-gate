#!/usr/bin/env bash
# Cong conformance `gate-v1`: chay MOI ca cua meta qua proxy that (internal/conformance), 0 skip, co -race.
#
#   bash cong/kiem-conformance.sh        (meta: DZ_META_DIR hoac ../meta; ghim: cong/hop-dong-ghim.json)
#
# MA THOAT: 0 dat · 1 TRUOT (co ca do / bo ca LECH ghim / so ca khac ghim) · 2 KHONG KIEM DUOC (khong co meta, khong co commit ghim,
# khong doc duoc ghim, khong co `=== RUN`, co ca BO QUA, 0 ca).
#
# GHIM: `cong/hop-dong-ghim.json` ghi SHA meta ma bo ca nay da duoc kiem tren. Bo ca `contracts/conformance/gate-v1` o meta (ca ca
# thay doi chua commit) KHAC commit ghim ⇒ do: them/doi ca = bump ghim O CUNG PR (README gate-v1, muc "Ghim"). `so_ca` la mau so: bo ca
# mat ca ma runner van xanh la xanh gia.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
command -v go >/dev/null 2>&1 || { echo "[MÃ 2] KHÔNG KIỂM ĐƯỢC — không có go"; exit 2; }

GHIM=cong/hop-dong-ghim.json
[ -f "$GHIM" ] || { echo "[MÃ 2] KHÔNG KIỂM ĐƯỢC — thiếu $GHIM"; exit 2; }
SHA=$(sed -n 's/.*"meta_sha": *"\([0-9a-f]\{7,64\}\)".*/\1/p' "$GHIM")
BO_CA=$(sed -n 's/.*"bo_ca": *"\([^"]*\)".*/\1/p' "$GHIM")
SO_CA=$(sed -n 's/.*"so_ca": *\([0-9]\+\).*/\1/p' "$GHIM")
if [ -z "$SHA" ] || [ -z "$BO_CA" ] || [ -z "$SO_CA" ]; then
  echo "[MÃ 2] KHÔNG KIỂM ĐƯỢC — $GHIM thiếu meta_sha/bo_ca/so_ca"
  exit 2
fi

META="${DZ_META_DIR:-../meta}"
if ! git -C "$META" rev-parse --git-dir >/dev/null 2>&1; then
  echo "[MÃ 2] KHÔNG KIỂM ĐƯỢC — không thấy kho meta ở '$META' (đặt DZ_META_DIR). Không có bộ ca thì không có gì để chạy."
  exit 2
fi
# Tuyet doi: `go test` chay o thu muc cua goi, duong dan tuong doi cua kho nay se tro sai cho.
META="$(cd "$META" && pwd)"
if ! git -C "$META" cat-file -e "${SHA}^{commit}" 2>/dev/null; then
  echo "[MÃ 2] KHÔNG KIỂM ĐƯỢC — kho meta ở '$META' không có commit ghim ${SHA:0:7} (git fetch?)"
  exit 2
fi
HEAD_META=$(git -C "$META" rev-parse --short HEAD)
echo "meta: $META @ $HEAD_META · ghim ${SHA:0:7} · bộ ca $BO_CA · mẫu số $SO_CA"

# Bo ca (cay lam viec, ke ca sua chua commit) phai GIONG HET commit ghim.
if ! git -C "$META" diff --quiet "$SHA" -- "$BO_CA"; then
  echo "[ĐỎ] bộ ca ở meta KHÁC commit ghim ${SHA:0:7}:"
  git -C "$META" diff --stat "$SHA" -- "$BO_CA" | tail -8
  echo "     Đổi/thêm ca = chạy lại cổng này rồi bump $GHIM (meta_sha, so_ca) CÙNG PR."
  exit 1
fi
N_TEP=$(find "$META/$BO_CA/cases" -name '*.json' 2>/dev/null | grep -c '')
if [ "$N_TEP" -ne "$SO_CA" ]; then
  echo "[ĐỎ] bộ ca có $N_TEP tệp, ghim khai $SO_CA"
  exit 1
fi

RA="$(mktemp)" || exit 2
trap 'rm -f "$RA"' EXIT
DZ_META_DIR="$META" go test -race -count=1 -v ./internal/conformance >"$RA" 2>&1
MA_GO=$?

if ! grep -q '^=== RUN' "$RA"; then
  echo "[MÃ 2] KHÔNG KIỂM ĐƯỢC — đầu ra không có \`=== RUN\` nào."
  head -20 "$RA"
  exit 2
fi
if grep -q 'KHÔNG KIỂM ĐƯỢC' "$RA"; then
  grep 'KHÔNG KIỂM ĐƯỢC' "$RA" | head -3
  exit 2
fi
DAT=$(grep -cE '^ +--- PASS: TestConformance/' "$RA")
TRUOT=$(grep -cE '^ +--- FAIL: TestConformance/' "$RA")
BO_QUA=$(grep -cE '^ *--- SKIP:' "$RA")
echo "ca: ${DAT} đạt · ${TRUOT} trượt · ${BO_QUA} bỏ qua (mẫu số ${SO_CA})"

if [ "$MA_GO" -ne 0 ] || [ "$TRUOT" -gt 0 ]; then
  grep -E '^ *--- FAIL:' "$RA" | head -30
  grep -vE '^(=== |\s*--- PASS)' "$RA" | head -40
  echo "[ĐỎ] conformance trượt (mã ${MA_GO})"
  exit 1
fi
if [ "$BO_QUA" -gt 0 ]; then
  echo "[MÃ 2] ${BO_QUA} ca BỎ QUA — bộ ca cấm skip (0 skip)."
  exit 2
fi
if [ "$DAT" -ne "$SO_CA" ]; then
  echo "[ĐỎ] ${DAT} ca đạt ≠ mẫu số ${SO_CA}: runner bỏ sót ca (hoặc ghim sai)."
  exit 1
fi
echo "[XANH] conformance gate-v1: ${DAT}/${SO_CA} ca, 0 bỏ qua, -race, meta ghim ${SHA:0:7}."
