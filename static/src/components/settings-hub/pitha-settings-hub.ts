// Forwards an old Settings link to the page that now holds its dialog
// (issues #805 and #895; docs/components/overview.md "SettingsHubPage"). A URL
// fragment never reaches the server, so `/settings#modal-jev`,
// `/settings#update-panel` and `/settings/operations#modal-sleep-guard` (a
// dialog that moved to another page) land on a page whose
// `[data-hash-routes]` attribute is the JSON map "hash id -> page path"
// (internal/web/settingspages.HashRoutes; the hub and every Settings page
// embed it). The hash is kept, so the destination page's pitha-modal opens the
// same dialog. A hash whose page is the current one stays: that page opens it.

/** The page path the hash id belongs to, or null for no/unknown hash. */
export function routeFor(routes: Record<string, string>, hash: string): string | null {
  let id: string;
  try {
    id = decodeURIComponent(hash.replace(/^#/, ''));
  } catch {
    return null;
  }
  return Object.hasOwn(routes, id) ? routes[id] : null;
}

/** Reads the hub's route map; `{}` when the attribute is missing or not a JSON object. */
export function readRoutes(root: ParentNode = document): Record<string, string> {
  const raw = root.querySelector('[data-hash-routes]')?.getAttribute('data-hash-routes');
  if (!raw) return {};
  try {
    const parsed: unknown = JSON.parse(raw);
    return parsed && typeof parsed === 'object' ? (parsed as Record<string, string>) : {};
  } catch {
    return {};
  }
}

/** Replaces the current page with the page holding the dialog the URL hash names, unless it is this page. */
export function forward(
  replace: (url: string) => void = (url) => location.replace(url),
  currentPath: string = location.pathname,
): void {
  const path = routeFor(readRoutes(), location.hash);
  if (path && path !== currentPath.replace(/\/+$/, '')) replace(path + location.hash);
}

forward();
