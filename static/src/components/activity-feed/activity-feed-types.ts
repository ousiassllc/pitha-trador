// Wire shapes and constants for `pitha-activity-feed`.
// Mirrors docs/api/endpoints.md §5 `GET /api/v1/activity` shapes.
export interface QueueStatus {
  queue: string;
  pending: number;
  running: number;
  failed_recent: number;
}

export interface ActivityEvent {
  type: string;
  timestamp: string;
  queue?: string;
  symbol?: string;
  detail: string;
  latency_ms?: number;
}

export interface ActivityAPIResponse {
  queues: QueueStatus[];
  events: ActivityEvent[];
  as_of: string;
}

// Mirrors docs/api/endpoints.md §6 `/ws/activity` message shapes.
export type ActivityWsMessage =
  | { type: 'job_update'; queue: string; pending: number; running: number; failed_recent?: number }
  | { type: 'activity_event'; event: ActivityEvent }
  | { type: 'resync' };

export const EVENT_TYPES = [
  'job',
  'jev_scout',
  'jev_trader',
  'kill_switch',
  'news_feed',
  'broker_notice',
] as const;
export const QUEUES = [
  'market-data',
  'feature-calc',
  'jev-scout',
  'jev-trader',
  'outcome-labeling',
  'analytics',
] as const;

// Feed size bounds (functional.md FR-ACT-3): the displayed list never
// grows past the API's maximum, however long the page stays open.
export const MAX_EVENTS = 500;
export const KILL_SWITCH_LIMIT = 10;
