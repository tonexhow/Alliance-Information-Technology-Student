# Electron wrapper

This wrapper keeps the normal AITS website as the online UI and packages a local copy of `client/dist` as the offline shell. Native cache/outbox records are stored in SQLite at the Electron user-data directory.

1. Build the web client: `cd client && npm ci && npm run build`
2. Install wrapper dependencies: `cd wrapper\electron && npm install`
3. Test: `npm start`
4. Windows installer: `npm run build:windows`

Set `AITS_APP_URL` before starting/building to override `https://aits.jpano.dev`.
The Windows build is written beneath `client/public/downloads/electron-build`; publish/copy the final installer as `client/public/downloads/AITS-Windows.exe` for the website install center.
