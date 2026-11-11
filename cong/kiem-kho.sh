#!/usr/bin/env bash
# Cong kiem kho `datazen-gate`: gofmt · go vet · go build · go test -race (0 ca bo qua), chay MOT lan, in so that.
#
#   bash cong/kiem-kho.sh
#
# MA THOAT: 0 dat · 1 TRUOT (gofmt lech / vet / build / co ca truot) · 2 KHONG KIEM DUOC (thieu go, 0 goi, 0 ca, co ca BO QUA).
# Luat ba trang thai cua workspace: "khong kiem duoc" KHONG phai "dat". Mot ca `t.Skip` im lang la xanh gia — kho nay cam: 0 skip.
#
# `go test -v` CO Y: phep dem ca bo qua doc `--- SKIP` — chi co trong dau ra chi tiet. DOI CHUNG DUONG: dau ra chi tiet LUON co it
# nhat mot `=== RUN`; vang no thi con so dem duoc la vo nghia (khong in "0 bo qua" noi doi) ⇒ ma 2.
set -uo pipefail
cd "$(dirname "$0")/.." || exit 2
command -v go >/dev/null 2>&1 || { echo "[MÃ 2] KHÔNG KIỂM ĐƯỢC — không có go"; exit 2; }

N=$(go list ./... 2>/dev/null | grep -c '' || true)
echo "số gói Go: ${N}"
if [ "$N" -eq 0 ]; then
  echo "[MÃ 2] KHÔNG KIỂM ĐƯỢC — kho không có gói Go nào (các cổng bên dưới sẽ 'xanh' vì không nhìn gì)."
  exit 2
fi

LECH=$(gofmt -l .)
if [ -n "$LECH" ]; then
  echo "$LECH"
  echo "[ĐỎ] gofmt lệch — chạy: gofmt -w ."
  exit 1
fi
echo "gofmt: sạch"

go vet ./... || { echo "[ĐỎ] go vet"; exit 1; }
echo "go vet: sạch"

go build ./... || { echo "[ĐỎ] go build"; exit 1; }
echo "go build: được"

RA_FILE="$(mktemp)" || exit 2
trap 'rm -f "$RA_FILE"' EXIT
# Bo ca conformance can kho meta nen chay RIENG (cong/kiem-conformance.sh, co ghim SHA); o day chi test cua chinh kho.
GOI=$(go list ./... | grep -v '/internal/conformance$')
# shellcheck disable=SC2086
go test -race -count=1 -v $GOI >"$RA_FILE" 2>&1
MA_GO=$?

if ! grep -q '^=== RUN' "$RA_FILE"; then
  echo "[MÃ 2] KHÔNG KIỂM ĐƯỢC — đầu ra không có \`=== RUN\` nào, nên không đếm được gì."
  head -20 "$RA_FILE"
  exit 2
fi

DAT=$(grep -cE '^ *--- PASS:' "$RA_FILE")
TRUOT=$(grep -cE '^ *--- FAIL:' "$RA_FILE")
BO_QUA=$(grep -cE '^ *--- SKIP:' "$RA_FILE")
echo "ca: ${DAT} đạt · ${TRUOT} trượt · ${BO_QUA} bỏ qua"

if [ "$MA_GO" -ne 0 ] || [ "$TRUOT" -gt 0 ]; then
  grep -E '^ *--- FAIL:|^(FAIL|panic:)' "$RA_FILE" | head -40
  echo "[ĐỎ] go test trượt (mã ${MA_GO})"
  exit 1
fi
if [ "$DAT" -eq 0 ]; then
  echo "[MÃ 2] KHÔNG KIỂM ĐƯỢC — 0 ca đạt."
  exit 2
fi
if [ "$BO_QUA" -gt 0 ]; then
  echo "[MÃ 2] mọi ca chạy đều đạt, NHƯNG ${BO_QUA} ca BỎ QUA — kho này cấm skip (xanh không có nghĩa là đã kiểm):"
  grep -B1 -E '^ *--- SKIP:' "$RA_FILE" | head -30
  exit 2
fi
echo "[XANH] ${N} gói, ${DAT} ca đạt, 0 bỏ qua."
