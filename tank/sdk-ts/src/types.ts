export interface TankConfig {
  baseURL: string;
  token: string;
  timeoutMs?: number;
}

export interface RequestOptions {
  signal?: AbortSignal;
}

export interface Shard {
  index: number;
  hash: string;
  node_url: string;
}

export interface Segment {
  index: number;
  size: number;
  merkle_root: string;
  shards: Shard[];
}

export interface Manifest {
  version: number;
  file_id: string;
  size: number;
  created_at: string;
  segments: Segment[];
}

export type RegistrationState =
  | "not_queued"
  | "pending"
  | "submitted"
  | "registered"
  | "failed";

export interface Registration {
  file_id: string;
  target: string;
  status: RegistrationState;
  transaction_hash?: string;
  attempts: number;
  last_error?: string;
}

export interface TankOptions extends RequestOptions {
  filename?: string;
}

export interface FileInfo {
  file_id: string;
  filename: string;
  size: number;
}
