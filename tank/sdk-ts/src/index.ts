export {
  Tank,
  APIError,
  IntegrityError,
  MAX_FILE_BYTES,
} from "./client.js";

export type {
  TankConfig,
  TankOptions,
  FileInfo,
  RequestOptions,
  Shard,
  Segment,
  Manifest,
  RegistrationState,
  Registration,
} from "./types.js";

export { encryptFile, decryptFile, parseRecovery, exportRecovery, isEncrypted,
  fileHash, MAX_PLAIN_BYTES } from "./encryption.js";
export type { Recovery } from "./encryption.js";
