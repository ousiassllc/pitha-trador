// Pure helpers for `pitha-activity-feed`: keeping WebSocket messages that
// arrived while a GET snapshot was in flight, and laying them over that
// snapshot when it lands. WS messages here are deltas (unlike
// pitha-scanner-table's full pushes), so a GET issued before them cannot
// simply be dropped - it would leave the snapshot unloaded - and one
// answered after them must not erase them.
import type {
  ActivityAPIResponse,
  ActivityEvent,
  ActivityWsMessage,
  QueueStatus,
} from './activity-feed-types';

type JobUpdate = Extract<ActivityWsMessage, { type: 'job_update' }>;

// One GET stream (snapshot or Kill Switch history): numbers its requests so
// only the latest may touch state, and collects the messages pushed while
// that latest request is in flight.
export class InFlightWs {
  private generation = 0;
  private messages: ActivityWsMessage[] = [];
  private active = false;

  begin(): number {
    this.messages = [];
    this.active = true;
    return ++this.generation;
  }

  record(message: ActivityWsMessage): void {
    if (this.active) this.messages.push(message);
  }

  // The messages pushed since `request` began, or null when a newer request
  // has superseded it (its result must then be dropped).
  finish(request: number): ActivityWsMessage[] | null {
    if (request !== this.generation) return null;
    const messages = this.messages;
    this.messages = [];
    this.active = false;
    return messages;
  }
}

export function applyJobUpdate(queues: QueueStatus[], message: JobUpdate): QueueStatus[] {
  return queues.map((q) =>
    q.queue === message.queue
      ? {
          ...q,
          pending: message.pending,
          running: message.running,
          failed_recent: message.failed_recent ?? q.failed_recent,
        }
      : q,
  );
}

const eventKey = (e: ActivityEvent): string =>
  JSON.stringify([e.type, e.timestamp, e.queue, e.symbol, e.detail, e.latency_ms]);

// Puts the pushed `activity_event`s (arrival order) in front of the fetched
// events (newest first). A pushed event the server already included in the
// response is not listed twice.
export function overlayEvents(
  fetched: ActivityEvent[],
  pushed: ActivityWsMessage[],
  limit: number,
): ActivityEvent[] {
  const events = pushed.flatMap((m) => (m.type === 'activity_event' ? [m.event] : [])).reverse();
  const seen = new Set(events.map(eventKey));
  return [...events, ...fetched.filter((e) => !seen.has(eventKey(e)))].slice(0, limit);
}

export function overlaySnapshot(
  response: ActivityAPIResponse,
  pushed: ActivityWsMessage[],
  limit: number,
): Pick<ActivityAPIResponse, 'queues' | 'events'> {
  return {
    queues: pushed.reduce(
      (queues, m) => (m.type === 'job_update' ? applyJobUpdate(queues, m) : queues),
      response.queues,
    ),
    events: overlayEvents(response.events, pushed, limit),
  };
}

// Mirrors the server's filter semantics (activityfeed.Service): a queue
// filter matches only job events on that queue.
export function matchesFilter(event: ActivityEvent, type: string, queue: string): boolean {
  if (type && event.type !== type) return false;
  if (queue && (event.type !== 'job' || event.queue !== queue)) return false;
  return true;
}
