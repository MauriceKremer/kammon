# Plan: KAMmon web-user accounts

**Status:** Completed.  
**Scope:** Let a KAMmon administrator manage the web login users stored in `kammon.yml`. This is separate from MeshCore room clients in [RoomAccounts.md](requirements/RoomAccounts.md) and the room-account work in [PLAN.md](PLAN.md) Slice 6.

## Goal

Manage the `users` list (`name`, password representation, `role`) from the browser, save changes to the existing YAML config, and apply them without restarting KAMmon. Keep file-based setup, use built-in PBKDF2 hashing so raw passwords are not stored, and preserve the current `admin` / `viewer` roles with enthusiast-level simplicity (no extra dependencies, no enterprise complexity).

## Boundaries

- This manages KAMmon logins only. It does not change room passwords, radio admission, or KAM's API.
- Store one-way password hashes using Go's built-in `crypto/pbkdf2` + `crypto/rand` + `crypto/subtle`. Never display stored hashes or passwords in screens, responses, or logs; password inputs are write-only.
- Pragmatic migration: allow existing plain-text `password:` lines in `kammon.yml` on read so existing hobbyist setups don't break, but when accounts are saved or updated through the UI, write out hashed credentials (e.g. `password_hash:`).
- Keep `/accounts` for the planned room-client page: it currently represents RoomAccounts in the sidebar placeholder and Slice 6. Give web-user management a clearly separate **Web users** navigation entry and route (proposed: `/web-users`).
- No database, password reset email, SSO, extra roles, complex key-rotation ceremonies, or config hot-reload for settings other than users.

## Slices

### 0. Requirements and decisions

- [x] Add a focused `docs/requirements/WebAccounts.md`, linked from `docs/requirements/Access.md` and this plan.
- [x] Specify administrator-only access, create/rename/change-role/change-password/delete behavior, field validation, confirmation for deletion, and success/error wording.
- [x] Require at least one administrator to remain after every account change. Preserve the existing startup configuration behavior unless separately decided; current config validation requires at least one user, not an admin.
- [x] Decide session behavior and document it: recommended simplest safe rule is to invalidate all sessions after any successful account change. This makes role/password/deletion changes effective immediately; the administrator must sign in again.
- [x] Reserve `/accounts` for room clients and use `/web-users` for these settings, avoiding a collision between web accounts and radio clients.

**Done when:** requirements and decisions state the invariants and account/session behavior before implementation starts.

### 1. Safe YAML update path and PBKDF2 auth check

- [x] Add compact password hashing/verification using Go stdlib (`crypto/pbkdf2` with SHA-256, 16-byte random salt, sensible moderate iterations like 100,000, formatted as `$pbkdf2-sha256$i=100000$salt$hash`).
- [x] Update `auth.Check` to verify hashes via `subtle.ConstantTimeCompare`, while falling back to plain-text check if an unmigrated `password:` entry is present.
- [x] Add a small config-layer operation for changing only the `users` list in the configured file; pass the config path from `cmd/kammon` to the server through its dependencies.
- [x] Serialize read/validate/write operations so concurrent admin submissions cannot overwrite one another.
- [x] Preserve every unrelated config setting (and existing comments where practical); validate the resulting user list using the same rules as startup.
- [x] Write to a temporary file beside the config and replace the original only after the complete write succeeds. A failure must leave both the file and running users unchanged.
- [x] Update the in-memory user list only after persistence succeeds. New logins should use the updated list immediately, without restart.
- [x] Add focused tests for valid updates, duplicate/empty names, invalid roles, empty passwords, removal of the last user/admin, unrelated-setting preservation, and failed writes.

**Done when:** the config update is tested independently and failed saves cannot leave a partial or half-applied account change.

### 2. Administrator page and routes

- [x] Add a **Web users** page at `/web-users`, with a table showing each user name and role, and controls to add, rename, change role, set a new password, and remove.
- [x] Never prefill or return passwords. Require a new password only when one is being set; do not make an unchanged password visible or inferable.
- [x] Register every page and mutation route as admin-only. Mutations use non-GET methods and the existing Origin/htmx checks; viewers receive `403` for every route.
- [x] Confirm destructive removal and explain that account changes end existing sessions (if the recommended all-sessions policy is adopted).
- [x] On validation or disk errors, retain the form where safe and show an actionable message; do not claim a change succeeded until the file was saved.
- [x] Add focused handler tests for the permission boundary, each account operation, failure responses, no password leakage, and session invalidation.

**Done when:** an administrator can manage users in the browser, viewers cannot, and the disk and running configuration agree after every result.

### 3. Integration and acceptance

- [x] Run `go test -race ./...`, `go vet ./...`, and `gofmt -l .`.
- [x] Run KAMmon in demo mode and check the new page in a browser in both themes, including keyboard focus, narrow layout, confirmations, and error/success feedback.
- [x] Verify a saved account can sign in immediately, a removed/changed account cannot keep an old session, and restarting KAMmon loads the saved accounts.
- [x] Update `docs/requirements/Access.md`, `docs/PLAN.md`, and any page/navigation tests to link the new feature without changing RoomAccounts requirements.

**Done when:** the acceptance checks pass, the plan and requirements agree with the behavior, and room-client account work remains a separate slice.

## Main risks to keep visible

- `config.Load` reads the YAML once at startup and `web.Server` currently receives a config value; runtime updates need an explicit, synchronized source for the live users and a config path for persistence.
- Sessions store the user and role at login. Updating the YAML alone does not revoke a deleted or demoted user's existing session.
- Re-encoding the whole YAML file carelessly could alter unrelated settings or discard comments. Limit the write to the `users` node and test preservation.
