// Preload for the Tailwind CLI (`node --require`): lets the build run in a
// checkout whose absolute path contains `#` (e.g. a worktree for branch
// `feat/#925`, issue #926).
//
// enhanced-resolve (used by @tailwindcss/node to resolve `@import`) escapes a
// literal `#` in resolved paths as `\0#` so it can't be mistaken for a URL
// fragment. Tailwind passes the result straight to fs without decoding it,
// so the build fails with ERR_INVALID_ARG_VALUE (path with null bytes).
// Decode the escape in the resolver's callback result. Paths without `#`
// are untouched, so normal builds are unaffected.
const Resolver = require('enhanced-resolve/lib/Resolver');

const resolve = Resolver.prototype.resolve;
Resolver.prototype.resolve = function patchedResolve(...args) {
  const last = args.length - 1;
  const callback = args[last];
  if (typeof callback === 'function') {
    args[last] = (err, result, ...rest) =>
      callback(err, typeof result === 'string' ? result.replaceAll('\0#', '#') : result, ...rest);
  }
  return resolve.apply(this, args);
};
