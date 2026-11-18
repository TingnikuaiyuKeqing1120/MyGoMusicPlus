// Build the web app with the config's version linked in: mygo's web compile
// carries no package flags, so without this the app reports no version and
// every release looks newer than it. The version stays in mygo.config.ts.
import { spawnSync } from "node:child_process";
import config from "../mygo.config.ts";

// The web shell keeps its existing package identity, even if this command
// follows a Windows package build in the same terminal.
delete process.env.MYGO_PLUS_DESKTOP;
const { version, identifier } = config({ command: "build" });
if (!version || !identifier) {
  console.error("mygo.config.ts has no version or identifier");
  process.exit(1);
}
const ldflags =
  `-X github.com/egoist/mygo.packageVersion=${version} ` +
  `-X github.com/egoist/mygo.packageIdentifier=${identifier}`;
const r = spawnSync("go", ["tool", "mygo", "build", "-platform", "web"], {
  cwd: new URL("..", import.meta.url),
  stdio: "inherit",
  env: { ...process.env, MYGO_LDFLAGS: ldflags },
});
process.exit(r.status ?? 1);
