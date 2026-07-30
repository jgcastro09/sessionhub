#!/usr/bin/env node
import { spawnSync } from "node:child_process";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const here = dirname(fileURLToPath(import.meta.url));
const executable = join(here, process.platform === "win32" ? "sessionhub-bin.exe" : "sessionhub-bin");
const result = spawnSync(executable, process.argv.slice(2), { stdio: "inherit", windowsHide: false });
if (result.error) {
  console.error(`Unable to start Session Hub: ${result.error.message}. Reinstall without --ignore-scripts.`);
  process.exit(1);
}
process.exit(result.status ?? 1);
