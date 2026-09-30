# Capacitor wrapper

The native Android/iOS shell opens the production AITS origin so existing secure cookies keep working. The site service worker provides the cached offline web shell after the first successful load, while Capacitor's local `offline.html` error page prevents a generic WebView/network error on a first offline launch. `@capacitor-community/sqlite` supplies the native SQLite plugin used by `client/src/global/deviceCache.ts` when Capacitor is present.

Android:
1. `cd client && npm ci && npm run build`
2. `cd ..\wrapper\capacitor && npm install`
3. `npm run android:init` (first time only)
4. `npm run android:sync`
5. `npm run android:open`, then build the APK in Android Studio.

Set `AITS_APP_URL` to override `https://aits.jpano.dev`.
After producing a release APK, publish/copy it as `client/public/downloads/AITS-Android.apk` so the website install center enables the Android download.

iOS source can be initialized with `npm run ios:init`, but producing a signed `.ipa` requires macOS, Xcode, and an Apple Developer signing setup.
