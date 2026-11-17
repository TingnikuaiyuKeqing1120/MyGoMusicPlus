// Copy the persistent web shell into the Android app's assets so the Kotlin
// WebView shell serves it over the asset loader's virtual https domain.
import { mkdir, cp, rm } from "node:fs/promises";
import { join } from "node:path";

const root = new URL("..", import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, "$1");
const src = join(root, "build", "web-shell");
const dst = join(root, "android", "app", "src", "main", "assets", "web");

await rm(dst, { recursive: true, force: true });
await mkdir(dst, { recursive: true });
await cp(src, dst, { recursive: true });
console.log("android assets populated from build/web-shell");