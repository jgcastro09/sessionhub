export function target(platform = process.platform, arch = process.arch) {
  const systems = { win32: "windows", darwin: "darwin", linux: "linux" };
  const architectures = { x64: "amd64", arm64: "arm64" };
  const goos = systems[platform];
  const goarch = architectures[arch];
  if (!goos || !goarch || (goos === "windows" && goarch === "arm64")) {
    throw new Error(`Session Hub does not publish a binary for ${platform}/${arch}`);
  }
  return { goos, goarch };
}

export function assetName(version, platform = process.platform, arch = process.arch) {
  const { goos, goarch } = target(platform, arch);
  return `sessionhub_${version.replace(/^v/, "")}_${goos}_${goarch}.tar.gz`;
}

export function checksumFor(text, filename) {
  for (const line of text.split(/\r?\n/)) {
    const fields = line.trim().split(/\s+/);
    if (fields.length >= 2 && fields.at(-1).replace(/^\*/, "") === filename) {
      if (!/^[a-fA-F0-9]{64}$/.test(fields[0])) {
        throw new Error(`Invalid SHA-256 entry for ${filename}`);
      }
      return fields[0].toLowerCase();
    }
  }
  throw new Error(`checksums.txt has no entry for ${filename}`);
}

export function extractTar(tar, executableName) {
  for (let offset = 0; offset + 512 <= tar.length;) {
    const header = tar.subarray(offset, offset + 512);
    if (header.every((value) => value === 0)) break;
    const name = header.subarray(0, 100).toString("utf8").replace(/\0.*$/, "");
    const sizeText = header.subarray(124, 136).toString("ascii").replace(/\0.*$/, "").trim();
    const size = Number.parseInt(sizeText || "0", 8);
    if (!Number.isFinite(size) || size < 0 || offset + 512 + size > tar.length) {
      throw new Error("Release archive contains an invalid tar entry");
    }
    const base = name.split("/").at(-1);
    if (base === executableName || base === `${executableName}.exe`) {
      return Buffer.from(tar.subarray(offset + 512, offset + 512 + size));
    }
    offset += 512 + Math.ceil(size / 512) * 512;
  }
  throw new Error(`Release archive does not contain ${executableName}`);
}
