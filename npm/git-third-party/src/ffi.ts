import koffi from "koffi";
import { resolvePlatformPaths } from "./resolve.js";

const SYMBOLS = [
  "gtp_init",
  "gtp_add",
  "gtp_set",
  "gtp_unset",
  "gtp_update",
  "gtp_list",
  "gtp_remove",
  "gtp_rename",
  "gtp_save_patch",
  "gtp_diff_patch",
  "gtp_info",
] as const;

type SymbolName = (typeof SYMBOLS)[number];

type CharPtr = unknown;

interface Bindings {
  call(symbol: SymbolName, request: unknown): unknown;
  version(): string;
}

let cached: Bindings | null = null;

function load(): Bindings {
  if (cached) return cached;
  const { libPath } = resolvePlatformPaths();
  const lib = koffi.load(libPath);

  // Opaque pointer for the JSON response. Declaring 'void *' keeps the
  // pointer untouched on return so we can free it after decoding.
  const PtrOut = "void *";

  const free = lib.func("gtp_free", "void", [PtrOut]) as (p: CharPtr) => void;
  const versionFn = lib.func("gtp_version", PtrOut, []) as () => CharPtr;

  const calls: Partial<Record<SymbolName, (req: string) => CharPtr>> = {};
  for (const name of SYMBOLS) {
    calls[name] = lib.func(name, PtrOut, ["const char *"]) as (
      req: string,
    ) => CharPtr;
  }

  function readAndFree(ptr: CharPtr): string {
    if (!ptr) {
      throw new Error("git-third-party: bridge returned NULL");
    }
    try {
      // -1 length: read until terminating NUL.
      return koffi.decode(ptr, "char", -1) as string;
    } finally {
      free(ptr);
    }
  }

  cached = {
    call(symbol, request) {
      const fn = calls[symbol];
      if (!fn) throw new Error(`git-third-party: unknown symbol '${symbol}'`);
      const ptr = fn(JSON.stringify(request));
      return JSON.parse(readAndFree(ptr));
    },
    version() {
      const ptr = versionFn();
      return readAndFree(ptr);
    },
  };
  return cached;
}

export function call(symbol: SymbolName, request: unknown): unknown {
  return load().call(symbol, request);
}

export function libraryVersion(): string {
  return load().version();
}
