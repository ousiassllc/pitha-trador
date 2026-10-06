import { describe, expect, test } from 'bun:test';
import { FakeWebSocket } from '../lib/ws-test-support';
import {
  buttonLabels,
  flush,
  installFakes,
  mount,
  type PanelElement,
  stateBody,
} from './kill-switch-test-support';

installFakes();

interface Pending {
  method: string;
  respond: (state: string) => void;
}

// Mounts a running panel, then holds every later request open so a test
// decides the order in which the responses arrive.
async function mountWithHeldRequests(): Promise<{ el: PanelElement; pending: Pending[] }> {
  const { el } = await mount('running');
  const pending: Pending[] = [];
  globalThis.fetch = ((_input: RequestInfo | URL, init?: RequestInit) =>
    new Promise<Response>((resolve) => {
      pending.push({
        method: init?.method ?? 'GET',
        respond: (state) => resolve(new Response(JSON.stringify(stateBody(state)))),
      });
    })) as typeof fetch;
  return { el, pending };
}

function wsPush(message: object): void {
  FakeWebSocket.instances[0].emit('message', { data: JSON.stringify(message) });
}

function click(el: PanelElement, label: string): void {
  const button = Array.from(el.shadowRoot?.querySelectorAll('button') ?? []).find(
    (b) => b.textContent?.trim() === label,
  );
  button?.click();
}

function shownStatus(el: PanelElement): string | null | undefined {
  return el.shadowRoot?.querySelector('[data-status]')?.getAttribute('data-status');
}

describe('pitha-kill-switch-panel response ordering', () => {
  test('a stale status response arriving after a kill_switch push does not revive running + Kill', async () => {
    const { el, pending } = await mountWithHeldRequests();
    let changed = 0;
    el.addEventListener('systemStateChanged', () => {
      changed += 1;
    });

    wsPush({ type: 'state_changed', state: 'running' });
    wsPush({ type: 'kill_switch', reason: 'risk' });
    expect(changed).toBe(1);
    expect(pending).toHaveLength(2);

    pending[0].respond('running');
    await flush(el);
    expect(shownStatus(el)).toBe('killed');
    expect(buttonLabels(el)).toEqual([]);
    expect(changed).toBe(1);

    pending[1].respond('killed');
    await flush(el);
    expect(shownStatus(el)).toBe('killed');
    expect(buttonLabels(el)).toEqual(['Resume']);
    expect(changed).toBe(2);
  });

  test('an older status response arriving after the action response does not overwrite it', async () => {
    const { el, pending } = await mountWithHeldRequests();
    let changed = 0;
    el.addEventListener('systemStateChanged', () => {
      changed += 1;
    });

    wsPush({ type: 'state_changed', state: 'running' });
    click(el, 'Pause');
    expect(pending.map((p) => p.method)).toEqual(['GET', 'POST']);

    pending[1].respond('paused');
    await flush(el);
    expect(shownStatus(el)).toBe('paused');
    expect(changed).toBe(1);

    pending[0].respond('running');
    await flush(el);
    expect(shownStatus(el)).toBe('paused');
    expect(buttonLabels(el)).toEqual(['Resume', 'Kill']);
    expect(changed).toBe(1);
  });

  test('an action response arriving after a kill_switch push is discarded', async () => {
    const { el, pending } = await mountWithHeldRequests();
    let changed = 0;
    el.addEventListener('systemStateChanged', () => {
      changed += 1;
    });

    click(el, 'Pause');
    wsPush({ type: 'kill_switch', reason: 'risk' });
    expect(changed).toBe(1);

    pending[0].respond('paused');
    await flush(el);
    expect(shownStatus(el)).toBe('killed');
    expect(changed).toBe(1);

    pending[1].respond('killed');
    await flush(el);
    expect(buttonLabels(el)).toEqual(['Resume']);
    expect(el.shadowRoot?.querySelector('[role="alert"]')).toBeNull();
  });

  test('the status badge is a polite live region so a Kill is announced', async () => {
    const { el } = await mount('running');

    const badge = el.shadowRoot?.querySelector('.pitha-kill-switch-panel-status');
    expect(badge?.getAttribute('role')).toBe('status');
    expect(badge?.getAttribute('aria-live')).toBe('polite');
  });
});
