export class GitThirdPartyError extends Error {
  readonly exitCode: number;
  readonly stdout: string;
  readonly stderr: string;

  constructor(exitCode: number, message: string, stdout = "", stderr = "") {
    super(message);
    this.name = "GitThirdPartyError";
    this.exitCode = exitCode;
    this.stdout = stdout;
    this.stderr = stderr;
  }
}

export class ConfigError extends GitThirdPartyError {
  constructor(exitCode: number, message: string, stdout?: string, stderr?: string) {
    super(exitCode, message, stdout, stderr);
    this.name = "ConfigError";
  }
}

export class NetworkError extends GitThirdPartyError {
  constructor(exitCode: number, message: string, stdout?: string, stderr?: string) {
    super(exitCode, message, stdout, stderr);
    this.name = "NetworkError";
  }
}

export class ConflictError extends GitThirdPartyError {
  constructor(exitCode: number, message: string, stdout?: string, stderr?: string) {
    super(exitCode, message, stdout, stderr);
    this.name = "ConflictError";
  }
}

export class CheckDirtyError extends GitThirdPartyError {
  constructor(exitCode: number, message: string, stdout?: string, stderr?: string) {
    super(exitCode, message, stdout, stderr);
    this.name = "CheckDirtyError";
  }
}

const BY_CODE: Record<number, typeof GitThirdPartyError> = {
  2: ConfigError,
  3: NetworkError,
  4: ConflictError,
  5: CheckDirtyError,
};

export interface BridgeResponse {
  exit_code?: number;
  error?: string;
  stdout?: string;
  stderr?: string;
  results?: unknown;
}

export function raiseFromResponse(resp: BridgeResponse): void {
  const code = resp.exit_code ?? 1;
  if (code === 0) return;
  const Cls = BY_CODE[code] ?? GitThirdPartyError;
  throw new Cls(
    code,
    resp.error || "git-third-party command failed",
    resp.stdout ?? "",
    resp.stderr ?? "",
  );
}
