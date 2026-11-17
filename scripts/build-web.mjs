// Build the web app with the config's version linked in: mygo's web compile
// carries no package flags, so without this the app reports no version and
// every release looks newer than it. The version stays in mygo.config.ts.
import { readFile } from "node:fs/promises";
import { spawnSync } from "node:child_process";

const root = new URL("..", import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, "$1");
const cfg = await readFile(root + "/mygo.config.ts", "utf8");
const pick = (key) => cfg.match(new RegExp(key + ':\\s*"([^"]+)"'))?.[1];
const version = pick("version");
const identifier = pick("identifier");
if (!version || !identifier) {
  console.error("mygo.config.ts has no version or identifier");
  process.exit(1);
}
const ldflags =
  `-X github.com/egoist/mygo.packageVersion=${version} ` +
  `-X github.com/egoist/mygo.packageIdentifier=${identifier}`;
const r = spawnSync("go", ["tool", "mygo", "build", "-platform", "web"], {
  stdio: "inherit",
  env: { ...process.env, MYGO_LDFLAGS: ldflags },
});
process.exit(r.status ?? 1);