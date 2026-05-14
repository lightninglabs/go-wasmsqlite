# Changelog

## Unreleased

- Replaced SQLite Worker1/Promiser runtime usage with a dedicated `sqlite3.oo1` Worker.
- Added browser E2E coverage for OPFS, memory mode, transactions, BLOBs, dump/load, migrations, generated SQL shapes, named parameters, context cancellation, and storage behavior.
- Updated SQLite WASM assets to 3.53.1 / 3530100.
- Added flat asset extraction/serving, relocated asset URL options, and app-wide cross-origin isolation helper.
- Added `RequirePersistent` / `require_persistent=true` to fail closed when persistent storage is required.
- Fixed empty BLOB handling, unsafe `int64` parameter binding, and opt-in `parse_time` scanning.
- Hardened the `golang-migrate` driver contract behavior.
- Added runtime protocol checks for bridge/worker asset mismatches.

Release tags should use semantic versioning. Deploy runtime JS/WASM files from the same module version as the Go code.
