// Sinh vector kiểm thử JWT/JWKS bằng `jose` (thư viện của @datazen-vn/sdk) để chứng minh phần xác minh viết bằng Go
// (internal/danhtinh) khớp một cài đặt độc lập. Chạy từ kho `sdk` (có jose trong node_modules):
//   node ../gate/testdata/sinh-vector.mjs ../gate/testdata
// Cần `npm ci` ở kho sdk để có jose. Sinh lại: khoá ngẫu nhiên mỗi lần ⇒ jwks.json và jwt-vectors.json phải commit CÙNG NHAU.
import { createHmac } from "node:crypto"
import { writeFileSync } from "node:fs"
import { createRequire } from "node:module"
import { join } from "node:path"

// Nạp `jose` từ thư mục HIỆN TẠI (kho sdk), không phải từ nơi đặt script này.
const { SignJWT, exportJWK, generateKeyPair } = await import(
  createRequire(join(process.cwd(), "noop.js")).resolve("jose")
)

const outDir = process.argv[2]
if (!outDir) throw new Error("dùng: node sinh-vector.mjs <thư-mục-đích>")

const NOW = new Date("2026-10-13T10:00:00Z")
const sec = (d) => Math.floor(d.getTime() / 1000)
const FAR = sec(new Date("2099-01-01T00:00:00Z"))
const HS_SECRET = "dz-conformance-hs256-secret-0123456789abcdef"

const keys = {}
for (const [kid, alg, opts] of [
  ["rsa-1", "RS256", { modulusLength: 2048 }],
  ["ps-1", "PS256", { modulusLength: 2048 }],
  ["ec-1", "ES256", {}],
  ["ec384-1", "ES384", {}],
  ["ed-1", "EdDSA", { crv: "Ed25519" }],
  ["rsa-khac", "RS256", { modulusLength: 2048 }],
]) {
  keys[kid] = { alg, ...(await generateKeyPair(alg, { extractable: true, ...opts })) }
}

const jwks = { keys: [] }
for (const [kid, k] of Object.entries(keys)) {
  // `rsa-khac` chỉ dùng để ký token SAI KHOÁ: không đưa vào JWKS.
  if (kid === "rsa-khac") continue
  jwks.keys.push({ ...(await exportJWK(k.publicKey)), kid, use: "sig" })
}

const claims = (over = {}) => ({ sub: "cus_1", ...over })
async function sign(kid, { payload = claims(), exp = FAR, iss = "https://app.test", aud = "app", header = {} } = {}) {
  let jwt = new SignJWT(payload).setProtectedHeader({ alg: keys[kid].alg, kid, ...header }).setIssuedAt(sec(NOW) - 60)
  if (iss) jwt = jwt.setIssuer(iss)
  if (aud) jwt = jwt.setAudience(aud)
  if (exp !== null) jwt = jwt.setExpirationTime(exp)
  return jwt.sign(keys[kid].privateKey)
}

const b64u = (o) => Buffer.from(typeof o === "string" ? o : JSON.stringify(o)).toString("base64url")
const hs = (payload, secret = HS_SECRET, alg = "HS256") => {
  const h = b64u({ alg, typ: "JWT" })
  const p = b64u(payload)
  const mac = createHmac(alg.replace("HS", "sha"), secret).update(`${h}.${p}`).digest("base64url")
  return `${h}.${p}.${mac}`
}
const base = { iss: "https://app.test", aud: "app", iat: sec(NOW) - 60 }

const tokens = {
  rs256: await sign("rsa-1", { payload: claims({ sub: "cus_rs" }) }),
  ps256: await sign("ps-1", { payload: claims({ sub: "cus_ps" }) }),
  es256: await sign("ec-1", { payload: claims({ sub: "cus_es" }) }),
  es384: await sign("ec384-1", { payload: claims({ sub: "cus_es384" }) }),
  eddsa: await sign("ed-1", { payload: claims({ sub: "cus_ed" }) }),
  rs256_sub_so: await sign("rsa-1", { payload: { sub: 12345 } }),
  rs256_sub_so_qua_lon: await sign("rsa-1", { payload: { sub: 9007199254740993 } }),
  rs256_sub_bool: await sign("rsa-1", { payload: { sub: true } }),
  rs256_claim_tuy_chinh: await sign("rsa-1", { payload: { uid: "cus_uid", sub: "khac" } }),
  rs256_thieu_sub: await sign("rsa-1", { payload: { foo: "bar" } }),
  het_han: await sign("rsa-1", { exp: sec(new Date("2026-10-13T09:00:00Z")) }),
  het_han_trong_dung_sai: await sign("rsa-1", { exp: sec(NOW) - 3 }),
  thieu_exp: await sign("rsa-1", { exp: null }),
  sai_aud: await sign("rsa-1", { aud: "app-khac" }),
  thieu_aud: await sign("rsa-1", { aud: null }),
  sai_iss: await sign("rsa-1", { iss: "https://ke-tan-cong.test" }),
  // Ký bằng khoá KHÁC nhưng khai kid của rsa-1.
  sai_khoa: await new SignJWT(claims())
    .setProtectedHeader({ alg: "RS256", kid: "rsa-1" })
    .setIssuedAt(sec(NOW) - 60)
    .setIssuer("https://app.test")
    .setAudience("app")
    .setExpirationTime(FAR)
    .sign(keys["rsa-khac"].privateKey),
  kid_la: await sign("rsa-1", { header: { kid: "khong-co" } }),
  khong_kid: await new SignJWT(claims()).setProtectedHeader({ alg: "ES256" }).setIssuedAt(sec(NOW) - 60).setIssuer("https://app.test").setAudience("app").setExpirationTime(FAR).sign(keys["ec-1"].privateKey),
  alg_none: `${b64u({ alg: "none", typ: "JWT" })}.${b64u({ ...base, sub: "cus_1", exp: FAR })}.`,
  // Tấn công nhầm thuật toán: HS256 ký bằng CHÍNH khoá công khai (dạng JSON của JWK) làm secret.
  hs256_bang_khoa_cong_khai: hs({ ...base, sub: "cus_1", exp: FAR }, JSON.stringify(jwks.keys[0]), "HS256"),
  hs256: hs({ ...base, sub: "cus_hs", exp: FAR }),
  hs256_het_han: hs({ ...base, sub: "cus_hs", exp: sec(new Date("2026-10-13T09:00:00Z")) }),
  hs256_sai_khoa: hs({ ...base, sub: "cus_hs", exp: FAR }, "mot-khoa-khac-dai-hon-32-byte-0123456789abcdef"),
  hs384: hs({ ...base, sub: "cus_hs384", exp: FAR }, HS_SECRET, "HS384"),
  rac: "khong.phai.token",
}
writeFileSync(join(outDir, "jwks.json"), JSON.stringify(jwks, null, 2) + "\n")
writeFileSync(
  join(outDir, "jwt-vectors.json"),
  JSON.stringify({ now: NOW.toISOString(), issuer: "https://app.test", audience: "app", hs_secret: HS_SECRET, tokens }, null, 2) + "\n",
)
console.log(`đã ghi ${jwks.keys.length} khoá + ${Object.keys(tokens).length} token vào ${outDir}`)
