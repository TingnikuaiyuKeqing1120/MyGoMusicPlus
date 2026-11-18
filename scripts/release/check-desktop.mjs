import assert from "node:assert/strict";
import { createHash, createPublicKey, verify } from "node:crypto";
import { readFileSync } from "node:fs";
import { join } from "node:path";
import config from "../../mygo.config.ts";

const [directory, target] = process.argv.slice(2);
const { version, updates, slug } = config({ command: "build" });
const manifest = JSON.parse(readFileSync(join(directory, `update-${target}.json`), "utf8"));
const archiveName = `${slug}-${version}-${target}.tar.gz`;
const archive = readFileSync(join(directory, archiveName));
assert.equal(manifest.version, version);
assert.equal(manifest.size, archive.length);
assert.equal(manifest.url, `https://github.com/${updates.github}/releases/download/v${version}/${archiveName}`);
const publicKey = createPublicKey({
  key: { kty: "OKP", crv: "Ed25519", x: Buffer.from(updates.publicKey, "base64").toString("base64url") },
  format: "jwk",
});
const digest = createHash("sha256").update(archive).digest();
assert.ok(verify(null, digest, publicKey, Buffer.from(manifest.signature, "base64")), "Invalid update signature");
console.log(`Verified ${archiveName} (${archive.length} bytes).`);
