export interface Entry {
  dir: string;
  url: string;
  follow?: string;
  pin?: string;
  subdir?: string;
  include?: string[];
  exclude?: string[];
  commit?: string;
  patched?: boolean;
  conflicts?: boolean;
}

export interface EntryResult {
  dir?: string;
  action?: string;
  url?: string;
  fromCommit?: string;
  toCommit?: string;
  newDir?: string;
  treePatch?: string;
  conflicts?: boolean;
  dryRun?: boolean;
  diff?: string;
}

interface RawEntryJSON {
  dir?: string;
  url?: string;
  follow?: string;
  pin?: string;
  subdir?: string;
  include?: string[] | null;
  exclude?: string[] | null;
  commit?: string;
  patched?: boolean;
  conflicts?: boolean;
}

interface RawEntryResultJSON {
  dir?: string;
  action?: string;
  url?: string;
  from_commit?: string;
  to_commit?: string;
  new_dir?: string;
  tree_patch?: string;
  conflicts?: boolean;
  dry_run?: boolean;
  diff?: string;
}

export function entryFromJSON(d: RawEntryJSON): Entry {
  return {
    dir: d.dir ?? "",
    url: d.url ?? "",
    follow: d.follow ?? "",
    pin: d.pin ?? "",
    subdir: d.subdir ?? "",
    include: d.include ?? [],
    exclude: d.exclude ?? [],
    commit: d.commit ?? "",
    patched: !!d.patched,
    conflicts: !!d.conflicts,
  };
}

export function entryResultFromJSON(d: RawEntryResultJSON): EntryResult {
  return {
    dir: d.dir ?? "",
    action: d.action ?? "",
    url: d.url ?? "",
    fromCommit: d.from_commit ?? "",
    toCommit: d.to_commit ?? "",
    newDir: d.new_dir ?? "",
    treePatch: d.tree_patch ?? "",
    conflicts: !!d.conflicts,
    dryRun: !!d.dry_run,
    diff: d.diff ?? "",
  };
}
