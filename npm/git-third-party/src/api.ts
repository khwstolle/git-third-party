import { call, libraryVersion } from "./ffi.js";
import {
  type Entry,
  type EntryResult,
  entryFromJSON,
  entryResultFromJSON,
} from "./types.js";
import { type BridgeResponse, raiseFromResponse } from "./errors.js";

interface CommonOpts {
  repoPath?: string;
}

interface MutatingOpts extends CommonOpts {
  dryRun?: boolean;
  commitMsg?: string;
}

interface Envelope {
  repo_path: string;
  dry_run: boolean;
  json_out: boolean;
  commit_msg: string;
  log_level: string;
  log_format: string;
  color: string;
  args: Record<string, unknown>;
}

function envelope(
  opts: { repoPath?: string; dryRun?: boolean; commitMsg?: string },
  args: Record<string, unknown> = {},
): Envelope {
  return {
    repo_path: opts.repoPath ?? ".",
    dry_run: !!opts.dryRun,
    json_out: true,
    commit_msg: opts.commitMsg ?? "",
    log_level: "",
    log_format: "",
    color: "",
    args,
  };
}

function parseResults(resp: BridgeResponse): unknown[] {
  const raw = resp.results;
  if (raw == null || raw === "") return [];
  if (Array.isArray(raw)) return raw;
  if (typeof raw === "object") return [raw];
  if (typeof raw === "string") {
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed : [parsed];
  }
  return [];
}

function firstEntryResult(resp: BridgeResponse): EntryResult {
  const rows = parseResults(resp);
  if (rows.length === 0) return {};
  return entryResultFromJSON(rows[0] as Parameters<typeof entryResultFromJSON>[0]);
}

function dispatch(symbol: string, env: Envelope): BridgeResponse {
  const resp = call(symbol as never, env) as BridgeResponse;
  raiseFromResponse(resp);
  return resp;
}

export function version(): string {
  return libraryVersion();
}

export function init(repoPath: string = "."): EntryResult {
  const resp = dispatch("gtp_init", envelope({ repoPath }));
  return firstEntryResult(resp);
}

export interface AddOpts extends MutatingOpts {
  dir: string;
  url: string;
  follow?: string;
  pin?: string;
  subdir?: string;
  include?: string[];
  exclude?: string[];
  allowDirExists?: boolean;
}

export function add(opts: AddOpts): EntryResult {
  const resp = dispatch(
    "gtp_add",
    envelope(opts, {
      url: opts.url,
      follow: opts.follow ?? "",
      pin: opts.pin ?? "",
      dir: opts.dir,
      subdir: opts.subdir ?? "",
      include: opts.include ?? [],
      exclude: opts.exclude ?? [],
      allow_dir_exists: !!opts.allowDirExists,
    }),
  );
  return firstEntryResult(resp);
}

export interface SetOpts extends MutatingOpts {
  dir: string;
  url?: string;
  follow?: string;
  pin?: string;
  subdir?: string;
  include?: string[];
  exclude?: string[];
}

export function set(opts: SetOpts): EntryResult {
  const args: Record<string, unknown> = {
    dir: opts.dir,
    url: opts.url ?? "",
    follow: opts.follow ?? "",
    pin: opts.pin ?? "",
  };
  if (opts.subdir !== undefined) args.subdir = opts.subdir;
  if (opts.include !== undefined) args.include = opts.include;
  if (opts.exclude !== undefined) args.exclude = opts.exclude;
  const resp = dispatch("gtp_set", envelope(opts, args));
  return firstEntryResult(resp);
}

export interface UnsetOpts extends MutatingOpts {
  dir: string;
  fields: string[];
}

export function unset(opts: UnsetOpts): EntryResult {
  const resp = dispatch(
    "gtp_unset",
    envelope(opts, { dir: opts.dir, fields: opts.fields }),
  );
  return firstEntryResult(resp);
}

export interface UpdateOpts extends MutatingOpts {
  dir?: string;
  check?: boolean;
}

export function update(opts: UpdateOpts = {}): EntryResult[] {
  const resp = dispatch(
    "gtp_update",
    envelope(opts, { dir: opts.dir ?? "", check: !!opts.check }),
  );
  return parseResults(resp).map((d) =>
    entryResultFromJSON(d as Parameters<typeof entryResultFromJSON>[0]),
  );
}

export interface ListOpts extends CommonOpts {
  dir?: string;
}

export function list(opts: ListOpts = {}): Entry[] {
  const resp = dispatch(
    "gtp_list",
    envelope(opts, { dir: opts.dir ?? "" }),
  );
  return parseResults(resp).map((d) =>
    entryFromJSON(d as Parameters<typeof entryFromJSON>[0]),
  );
}

export interface RemoveOpts extends MutatingOpts {
  dir: string;
}

export function remove(opts: RemoveOpts): EntryResult {
  const resp = dispatch("gtp_remove", envelope(opts, { dir: opts.dir }));
  return firstEntryResult(resp);
}

export interface RenameOpts extends MutatingOpts {
  dir: string;
  newDir: string;
  allowDirExists?: boolean;
}

export function rename(opts: RenameOpts): EntryResult {
  const resp = dispatch(
    "gtp_rename",
    envelope(opts, {
      dir: opts.dir,
      new_dir: opts.newDir,
      allow_dir_exists: !!opts.allowDirExists,
    }),
  );
  return firstEntryResult(resp);
}

export interface SavePatchOpts extends MutatingOpts {
  dir: string;
}

export function savePatch(opts: SavePatchOpts): EntryResult {
  const resp = dispatch(
    "gtp_save_patch",
    envelope(opts, { dir: opts.dir }),
  );
  return firstEntryResult(resp);
}

export interface DiffPatchOpts extends CommonOpts {
  dir: string;
}

export function diffPatch(opts: DiffPatchOpts): EntryResult {
  const resp = dispatch(
    "gtp_diff_patch",
    envelope(opts, { dir: opts.dir }),
  );
  return firstEntryResult(resp);
}

export interface InfoOpts extends CommonOpts {
  dir: string;
}

export function info(opts: InfoOpts): Entry {
  const resp = dispatch("gtp_info", envelope(opts, { dir: opts.dir }));
  const rows = parseResults(resp);
  if (rows.length === 0) {
    throw new Error(`info: no entry for ${opts.dir}`);
  }
  return entryFromJSON(rows[0] as Parameters<typeof entryFromJSON>[0]);
}
