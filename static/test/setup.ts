// Preloaded by `bun test` (see bunfig.toml) so that lib/ modules relying on
// browser globals (`document`, `WebSocket`) work under the Bun test runner,
// which does not provide a DOM by default.
import { GlobalRegistrator } from '@happy-dom/global-registrator';

GlobalRegistrator.register();
