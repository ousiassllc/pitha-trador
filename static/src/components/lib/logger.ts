// Structured console logger used by every Lit component
// (docs/components/overview.md §6). Fatal errors get a dedicated call so a
// future hook can forward them to the Go backend; the MVP only writes to
// the browser (WebView) console.

export type LogLevel = 'debug' | 'info' | 'warn' | 'error';

export interface LogFields {
  [key: string]: unknown;
}

function write(level: LogLevel, message: string, fields?: LogFields): void {
  const entry = { level, message, timestamp: new Date().toISOString(), ...fields };
  console[level](entry);
}

export const logger = {
  debug: (message: string, fields?: LogFields) => write('debug', message, fields),
  info: (message: string, fields?: LogFields) => write('info', message, fields),
  warn: (message: string, fields?: LogFields) => write('warn', message, fields),
  error: (message: string, fields?: LogFields) => write('error', message, fields),
  // Reserved for errors that should eventually be forwarded to the Go
  // backend (docs/components/overview.md §6); the MVP behavior is
  // identical to `error()` aside from the `fatal: true` marker field.
  fatal: (message: string, fields?: LogFields) =>
    write('error', message, { ...fields, fatal: true }),
};
