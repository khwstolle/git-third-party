export {
  add,
  diffPatch,
  info,
  init,
  list,
  remove,
  rename,
  savePatch,
  set,
  unset,
  update,
  version,
  type AddOpts,
  type DiffPatchOpts,
  type InfoOpts,
  type ListOpts,
  type RemoveOpts,
  type RenameOpts,
  type SavePatchOpts,
  type SetOpts,
  type UnsetOpts,
  type UpdateOpts,
} from "./api.js";

export {
  type Entry,
  type EntryResult,
} from "./types.js";

export {
  CheckDirtyError,
  ConfigError,
  ConflictError,
  GitThirdPartyError,
  NetworkError,
} from "./errors.js";

export { resolvePlatformPaths, resolveBinPath } from "./resolve.js";
