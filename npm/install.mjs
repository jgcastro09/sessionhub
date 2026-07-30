import { createHash } from "node:crypto";
import { chmod, copyFile, mkdir, rm, writeFile } from "node:fs/promises";
import { gunzipSync } from "node:zlib";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { assetName, checksumFor, extractTar } from "./lib/platform.mjs";

const packageRoot = dirname(fileURLToPath(import.meta.url));
const packageJSON = JSON.parse(await (await import("node:fs/promises")).readFile(join(packageRoot, "package.json"), "utf8"));
const version = packageJSON.version;
const asset = assetName(version);
const releaseBase = `https://github.com/nodestage/sessionhub/releases/download/v${version}`;

async function download(url, maximum) {
  const response = await fetch(url, {
    headers: { "user-agent": `sessionhub-npm/${version}` },
    redirect: "follow"
  });
  if (!response.ok) throw new Error(`Download failed (${response.status}) for ${url}`);
  const data = Buffer.from(await response.arrayBuffer());
  if (data.length > maximum) throw new Error(`Download exceeded ${maximum} bytes`);
  return data;
}

async function install() {
  const [archive, checksums] = await Promise.all([
    download(`${releaseBase}/${asset}`, 256 * 1024 * 1024),
    download(`${releaseBase}/checksums.txt`, 2 * 1024 * 1024)
  ]);
  const expected = checksumFor(checksums.toString("utf8"), asset);
  const actual = createHash("sha256").update(archive).digest("hex");
  if (actual !== expected) throw new Error(`Checksum mismatch for ${asset}`);

  const executableName = process.platform === "win32" ? "sessionhub-bin.exe" : "sessionhub-bin";
  const executable = join(packageRoot, "bin", executableName);
  await mkdir(dirname(executable), { recursive: true });
  await writeFile(executable, extractTar(gunzipSync(archive), "sessionhub"), { mode: 0o755 });
  if (process.platform !== "win32") await chmod(executable, 0o755);

  // A global install receives a native command at the prefix, so Session Hub
  // does not need Node.js after installation. The JS bin remains a local/npx
  // fallback and provides an objective diagnostic when postinstall was skipped.
  if (process.env.npm_config_global === "true" && process.env.npm_config_prefix) {
    const nativeCommand = process.platform === "win32"
      ? join(process.env.npm_config_prefix, "sessionhub.exe")
      : join(process.env.npm_config_prefix, "bin", "sessionhub");
    await mkdir(dirname(nativeCommand), { recursive: true });
    await rm(nativeCommand, { force: true });
    await copyFile(executable, nativeCommand);
    if (process.platform !== "win32") await chmod(nativeCommand, 0o755);
  }
  console.log(`Session Hub ${version} installed for ${process.platform}/${process.arch}`);
}

install().catch((error) => {
  console.error(`Session Hub installation failed: ${error.message}`);
  process.exitCode = 1;
});
