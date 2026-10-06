// Shared fixtures for the pitha-kill-switch-panel tests: a fake WebSocket,
// the server-side state rules, a mount helper and the per-test global
// setup/teardown.
import { afterEach, beforeEach, mock } from 'bun:test';
import { installFakeWebSocket } from '../lib/ws-test-support';
import './pitha-kill-switch-panel';

export type PanelElement = HTMLElement & { updateComplete: Promise<boolean> };

let originalFetch: typeof fetch;
let restoreWebSocket: () => void;

// installFakes registers the beforeEach/afterEach pair that swaps in
// FakeWebSocket and restores the real globals; call it once per test file.
export function installFakes(): void {
  beforeEach(() => {
    originalFetch = globalThis.fetch;
    restoreWebSocket = installFakeWebSocket();
    document.body.innerHTML = '';
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    restoreWebSocket();
    document.body.innerHTML = '';
  });
}

// Mirrors pitha-scanner-table.test.ts's nextMacrotask/flush: loadInitial()
// is fire-and-forget from connectedCallback, so a single real macrotask
// tick is the deterministic way to let its promise chain settle before
// asserting (microtasks-before-macrotasks ordering, not a guessed delay).
async function nextMacrotask(): Promise<void> {
  const { promise, resolve } = Promise.withResolvers<void>();
  setTimeout(resolve, 0);
  await promise;
}

export async function flush(el: PanelElement): Promise<void> {
  await nextMacrotask();
  await el.updateComplete;
}

export const URLS = {
  'status-url': '/api/v1/system/status',
  'pause-url': '/api/v1/system/pause',
  'resume-url': '/api/v1/system/resume',
  'kill-url': '/api/v1/system/kill',
  'ws-url': '/ws/system',
};

// Server-side transition rules (domain.SystemState.CanPause/CanResume/
// CanKill); the component must take these from responses, not derive them.
export function stateBody(state: string, flags: Partial<Record<string, boolean>> = {}) {
  const rules: Record<string, [boolean, boolean, boolean]> = {
    running: [true, false, true],
    paused: [false, true, true],
    killed: [false, true, false],
  };
  const [canPause, canResume, canKill] = rules[state] ?? [false, false, false];
  return {
    state,
    can_pause: canPause,
    can_resume: canResume,
    can_kill: canKill,
    ...flags,
  };
}

export function buttonLabels(el: PanelElement): (string | undefined)[] {
  return Array.from(el.shadowRoot?.querySelectorAll('button') ?? []).map((b) =>
    b.textContent?.trim(),
  );
}

// mount stubs `fetch` to answer every request with bodies[url] (default:
// the `state` rules above), appends a <pitha-kill-switch-panel> carrying
// the server-injected URL attributes, and waits for its initial render.
export async function mount(state: string, attrs: Record<string, string> = {}) {
  const fetchMock = mock((input: RequestInfo | URL) => {
    void input;
    return Promise.resolve(new Response(JSON.stringify(stateBody(state))));
  });
  globalThis.fetch = fetchMock as unknown as typeof fetch;

  const el = document.createElement('pitha-kill-switch-panel') as PanelElement;
  for (const [name, value] of Object.entries({ ...URLS, ...attrs })) {
    el.setAttribute(name, value);
  }
  document.body.appendChild(el);
  await flush(el);
  return { el, fetchMock };
}
