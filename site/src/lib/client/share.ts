// «Поделиться»: код сжимается deflate и кладётся в #-фрагмент ссылки.
// Сервер для этого не нужен, а фрагмент не уходит в логи nginx.

const PARAM = "code";

async function pipe(data: Uint8Array, stream: CompressionStream | DecompressionStream): Promise<Uint8Array> {
  const out = new Blob([data as BlobPart]).stream().pipeThrough(stream);
  return new Uint8Array(await new Response(out).arrayBuffer());
}

function toBase64Url(bytes: Uint8Array): string {
  let s = "";
  for (const b of bytes) s += String.fromCharCode(b);
  return btoa(s).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

function fromBase64Url(s: string): Uint8Array {
  const b64 = s.replace(/-/g, "+").replace(/_/g, "/");
  return Uint8Array.from(atob(b64), (c) => c.charCodeAt(0));
}

export async function encode(code: string): Promise<string> {
  return toBase64Url(await pipe(new TextEncoder().encode(code), new CompressionStream("deflate-raw")));
}

export async function decode(token: string): Promise<string> {
  return new TextDecoder().decode(await pipe(fromBase64Url(token), new DecompressionStream("deflate-raw")));
}

/** Ссылка на текущую страницу с кодом во фрагменте. */
export async function shareURL(code: string, base: string): Promise<string> {
  const url = new URL(base);
  url.hash = `${PARAM}=${await encode(code)}`;
  return url.toString();
}

/** Код из фрагмента ссылки, если он там есть и читается. */
export async function fromHash(hash: string): Promise<string | null> {
  const token = new URLSearchParams(hash.replace(/^#/, "")).get(PARAM);
  if (!token) return null;
  try {
    return await decode(token);
  } catch {
    return null;
  }
}
