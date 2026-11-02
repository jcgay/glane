# iPhone

An iPhone app searches your index offline, read-only, over the copy of
`glane.db` a sync app keeps on the phone. It shows the same web UI as
`glane serve --read-only`, with full-text search only (no semantic search).

iOS lets no app run another program, so unlike [Android](android.md) the
app doesn't start a server: the Go code is linked into it and answers the
web view directly. Nothing listens on a port, and no other app can reach
your index.

1. Get `glane.db` onto the phone, in a folder the Files app shows:
   [Möbius Sync](https://www.mobiussync.com) (Syncthing for iOS) with the
   `~/Sync/glane` folder set to **Receive Only**, or iCloud Drive if your
   computer keeps the folder there. With iCloud Drive, open the folder in
   Files once so the file is downloaded, not just listed.
2. Install the app. There is no release to download: Apple only installs
   apps it has signed, so you build it yourself with a Mac, Xcode and an
   Apple ID, see [Development](development.md#iphone-app).
3. Open glane, tap **Choose folder** and pick the folder that holds
   `glane.db`. The app remembers it.

When the sync app delivers a newer copy, the next search uses it.

The app speaks French when the phone does, and English otherwise.
