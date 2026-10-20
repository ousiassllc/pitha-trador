// Forwards a pre-#805 Settings link to the page that now holds its dialog
// (issue #805; docs/components/overview.md "SettingsHubPage"). A URL fragment
// never reaches the server, so `/settings#modal-jev` and
// `/settings#update-panel` land on the hub, whose
// `[data-hash-routes]` attribute is the JSON map "hash id -> page path"
// (internal/web/settingspages.HashRoutes). The hash is kept, so the
// destination page's pitha-modal opens the same dialog.

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

/** Replaces the hub with the page holding the dialog the URL hash names. */
export function forward(replace: (url: string) => void = (url) => location.replace(url)): void {
  const path = routeFor(readRoutes(), location.hash);
  if (path) replace(path + location.hash);
}

forward();
