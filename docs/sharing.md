# Sharing across machines

`glane` is one SQLite file, so sharing it across your machines is just keeping
that one file in sync — there's no server to run. Point `GLANE_DB` at a synced
folder on each machine.

This assumes **one machine at a time** (e.g. your laptop in the morning, your
desktop in the evening). Two machines writing at once is not supported.

1. Put the database in a folder replicated by a file-sync tool.
   [Syncthing](https://syncthing.net) is a good fit — free, no account,
   peer-to-peer between your own machines (`brew install syncthing`). Dropbox or
   iCloud Drive work too.
2. On **every** machine, point `glane` at the shared file:

   ```sh
   export GLANE_DB="$HOME/Sync/glane/glane.db"
   ```

Three rules keep the shared file consistent:

- **Wait for the sync to finish before switching machines.** Start `glane` on
  the second machine before the first has finished uploading and both copies
  diverge: the sync tool then writes a conflict copy
  (`glane.sync-conflict-….db`, or "conflicted copy" on Dropbox) and you must
  pick one by hand — SQLite files don't merge.
- **Don't enable WAL mode.** `glane` uses SQLite's default rollback journal, so
  a cleanly-closed database is a single `glane.db` file — exactly what syncs
  reliably. WAL would add `-wal`/`-shm` files that must stay in lockstep.
- **Keep the file downloaded, not "on-demand".** iCloud Drive and Google Drive
  can replace the file with a placeholder to save space, and SQLite can't open a
  placeholder. Mark the glane folder "keep downloaded" (a plain Syncthing or
  Dropbox folder doesn't have this issue).
