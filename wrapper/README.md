# AITS Native Wrappers

This folder keeps native shells separate from the existing `client/`, `server/`, and `data/` application structure.

- `electron/` - Windows/Linux/macOS desktop wrapper. It uses SQLite for the native device cache and falls back to the bundled web shell when the server is unreachable.
- `capacitor/` - Android/iOS wrapper. It uses the Capacitor Community SQLite bridge for the native device cache.

Both wrappers default to `https://aits.jpano.dev`. Override the remote origin with `AITS_APP_URL` (or `DOMAIN`) when building/running against another deployment.

Build the web client first (`client/npm run build`), then run the wrapper's `sync:web` command before starting or packaging it. See each wrapper's README for platform-specific steps.
