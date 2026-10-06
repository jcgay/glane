# Android

An Android app searches your index offline, read-only, over the copy of
`glane.db` that Syncthing keeps on the phone. It wraps `glane serve
--read-only` and shows the same web UI; semantic search is off there
(full-text only).

1. Share the database folder with the phone: install
   [Syncthing-Fork](https://f-droid.org/packages/com.github.catfriend1.syncthingfork/)
   from F-Droid, add the `~/Sync/glane` folder and set it to **Receive
   Only** on the phone, so the phone can never send a change back.
2. Install the app: download the APK from the
   [releases](https://github.com/jcgay/glane/releases) on the phone and open
   it (Android asks to allow installing apps from that source), or run
   `adb install -r <apk>` from a computer. Building one yourself is covered
   in [Development](development.md#android-app).

3. Open glane, grant "All files access" (needed to read the Syncthing
   folder), and adjust the database path if it isn't
   `/storage/emulated/0/Sync/glane/glane.db`.

When Syncthing delivers a newer copy, the next search uses it, with no
restart.

The server listens on `127.0.0.1` with a random port and no password, so
another app on the phone could find it and read your index (never change
it). Only install apps you trust next to it.

The app speaks the phone's language when it is French, and English
otherwise, like the web UI.
