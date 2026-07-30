import test from "node:test";
import assert from "node:assert/strict";
import { assetName, checksumFor, target } from "../lib/platform.mjs";

test("maps all published targets", () => {
  assert.deepEqual(target("win32", "x64"), { goos: "windows", goarch: "amd64" });
  assert.deepEqual(target("darwin", "arm64"), { goos: "darwin", goarch: "arm64" });
  assert.deepEqual(target("linux", "x64"), { goos: "linux", goarch: "amd64" });
  assert.equal(assetName("v1.2.3", "linux", "arm64"), "sessionhub_1.2.3_linux_arm64.tar.gz");
});

test("rejects unsupported targets", () => {
  assert.throws(() => target("freebsd", "x64"), /does not publish/);
  assert.throws(() => target("win32", "arm64"), /does not publish/);
});

test("requires an exact valid checksum", () => {
  const hash = "a".repeat(64);
  assert.equal(checksumFor(`${hash}  artifact.tar.gz\n`, "artifact.tar.gz"), hash);
  assert.throws(() => checksumFor(`${hash}  other\n`, "artifact.tar.gz"), /no entry/);
});
