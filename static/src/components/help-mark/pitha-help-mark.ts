// Opens and places the 「?」 help text of `atoms.HelpMark` and of the Lit
// helper `renderHelpMark` (lib/help-mark.ts) (issue #902). The help text
// (`[data-help-mark-text]`) is always in the DOM and tied to its button by
// `aria-describedby`, so screen readers read it without this script; this
// module only shows it visually:
//   - mouse hover (pointerType mouse/pen) and keyboard focus open it;
//   - a click on the button toggles it, which is how a tap opens it where a
//     tap neither hovers nor focuses (iOS Safari);
//   - Esc closes every open help text (WCAG 1.4.13 "dismissable"), a press
//     elsewhere closes the others, and the text stays open while the pointer
//     moves from the button onto it ("hoverable").
// The text is `position: fixed`, so a dialog's or table's overflow never clips
// it; it is placed under the button (above when there is no room below) and
// kept inside the viewport.
//
// The listeners sit on `document` and find the mark through
// `event.composedPath()`, so a mark inside a Lit component's shadow root
// (whose events are retargeted to the host at the document) works the same.
// Open marks are tracked in a set rather than found with `querySelectorAll`,
// which does not cross shadow roots either.

type Flag = 'hover' | 'focus' | 'pinned';

const flags = new Map<HTMLElement, Set<Flag>>();

const MARGIN = 8;

const isElement = (node: EventTarget | null): node is HTMLElement => node instanceof HTMLElement;

/** The `[data-help-mark]` ancestor of the event's real (pre-retargeting) target, if any. */
function markOf(event: Event): HTMLElement | null {
  for (const node of event.composedPath()) {
    if (isElement(node) && node.hasAttribute('data-help-mark')) return node;
  }
  return null;
}

function textOf(mark: HTMLElement): HTMLElement | null {
  return mark.querySelector<HTMLElement>('[data-help-mark-text]');
}

/** Puts the help text under the button (above if it does not fit), inside the viewport. */
function place(mark: HTMLElement): void {
  const button = mark.querySelector<HTMLElement>('[data-help-mark-button]');
  const text = textOf(mark);
  if (!button || !text) return;
  const anchor = button.getBoundingClientRect();
  const width = text.offsetWidth;
  const height = text.offsetHeight;
  const left = Math.max(MARGIN, Math.min(anchor.left, window.innerWidth - width - MARGIN));
  const below = anchor.bottom;
  const above = anchor.top - height;
  const top = below + height + MARGIN > window.innerHeight && above >= MARGIN ? above : below;
  text.style.left = `${left}px`;
  text.style.top = `${top}px`;
}

function render(mark: HTMLElement): void {
  const text = textOf(mark);
  if (!text) return;
  if ((flags.get(mark)?.size ?? 0) > 0) {
    text.setAttribute('data-open', '');
    place(mark);
  } else {
    flags.delete(mark);
    text.removeAttribute('data-open');
  }
}

function set(mark: HTMLElement, flag: Flag, on: boolean): void {
  const current = flags.get(mark) ?? new Set<Flag>();
  if (on) current.add(flag);
  else current.delete(flag);
  flags.set(mark, current);
  render(mark);
}

function closeAll(except: HTMLElement | null = null): void {
  for (const mark of [...flags.keys()]) {
    if (mark === except) continue;
    flags.delete(mark);
    render(mark);
  }
}

function isPointer(event: Event): boolean {
  const type = (event as PointerEvent).pointerType;
  return type === 'mouse' || type === 'pen';
}

/** Whether the node the pointer/focus moves to is still inside `mark` (also across a shadow root). */
function stillInside(mark: HTMLElement, next: EventTarget | null): boolean {
  return next instanceof Node && mark.contains(next);
}

export function onPointerOver(event: Event): void {
  const mark = markOf(event);
  if (mark && isPointer(event)) set(mark, 'hover', true);
}

export function onPointerOut(event: Event): void {
  const mark = markOf(event);
  if (!mark || !isPointer(event)) return;
  if (stillInside(mark, (event as PointerEvent).relatedTarget)) return;
  set(mark, 'hover', false);
}

export function onFocusIn(event: Event): void {
  const mark = markOf(event);
  if (mark) set(mark, 'focus', true);
}

export function onFocusOut(event: Event): void {
  const mark = markOf(event);
  if (!mark) return;
  if (stillInside(mark, (event as FocusEvent).relatedTarget)) return;
  flags.delete(mark);
  render(mark);
}

export function onClick(event: Event): void {
  const mark = markOf(event);
  const onButton = event
    .composedPath()
    .some((n) => isElement(n) && n.hasAttribute('data-help-mark-button'));
  if (!mark || !onButton) return;
  set(mark, 'pinned', !flags.get(mark)?.has('pinned'));
}

export function onPointerDown(event: Event): void {
  closeAll(markOf(event));
}

export function onKeyDown(event: Event): void {
  if ((event as KeyboardEvent).key === 'Escape') closeAll();
}

/** Keeps an open text attached to its button when the page scrolls or resizes. */
export function onViewportChange(): void {
  for (const mark of flags.keys()) place(mark);
}

export function init(): void {
  document.addEventListener('pointerover', onPointerOver);
  document.addEventListener('pointerout', onPointerOut);
  document.addEventListener('focusin', onFocusIn);
  document.addEventListener('focusout', onFocusOut);
  document.addEventListener('click', onClick);
  document.addEventListener('pointerdown', onPointerDown, true);
  document.addEventListener('keydown', onKeyDown);
  window.addEventListener('resize', onViewportChange);
  window.addEventListener('scroll', onViewportChange, true);
}

init();
