# KAMmon Requirements: Web Users

**Status:** Requirements defined. Slices 0-3 planned in [../PLAN-WebAccounts.md](../PLAN-WebAccounts.md).
**Scope:** Managing KAMmon web login users in `kammon.yml` from the browser. Web users log in to KAMmon and are completely separate from MeshCore room clients ([RoomAccounts.md](RoomAccounts.md)). Authentication rules and session mechanics belong to [Access.md](Access.md).

## 1. Summary

KAMmon web logins are configured in `kammon.yml` with two roles: `viewer` and `admin` ([Access.md](Access.md)). This feature lets administrators view, add, rename, change role, set passwords for, and delete web users through a dedicated web page (`/web-users`).

To preserve enthusiast-level simplicity without external dependencies, passwords updated through the web UI are hashed using Go's built-in `crypto/pbkdf2` with SHA-256 and stored as `password_hash` in `kammon.yml`. Existing plain-text `password:` entries in `kammon.yml` remain valid on read for backward compatibility.

In demo mode (`-demo`), changes are kept in memory for the duration of the process.

## 2. Goals and non-goals

**Goals**

- GL-1. Administrators can manage web logins from the browser without editing YAML by hand.
- GL-2. Password changes use built-in PBKDF2 hashing so plain-text passwords are not saved.
- GL-3. Legacy configurations with plain-text `password:` keep working without migration friction.
- GL-4. Changing or deleting an account revokes active sessions for that account immediately; the acting administrator remains signed in.
- GL-5. At least one administrator account must always remain configured.

**Non-goals**

- Managing radio/room protocol roles or room passwords (those live in KAM's `kam.yml` and [RoomAccounts.md](RoomAccounts.md)).
- Complex enterprise identity: SSO, email verification, password reset tokens, multi-factor authentication, or external user databases.
- Hot-reloading non-user config fields (polling intervals, ports, channels).

## 3. What the page shows

| Area | Content |
|---|---|
| **User table** | List of configured users: name, role (`admin` or `viewer`), and action buttons (Edit, Delete). Existing password hashes are never shown. |
| **User form** | Name input, role selector (`admin`, `viewer`), password input (required for new user, optional for edit to keep existing password). |
| **Notice / Invariants** | Clear notice: "Passwords are saved as PBKDF2 hashes in kammon.yml. At least one administrator is required." |
| **Demo badge** | In demo mode: "Demo mode: changes apply in memory and are discarded on restart." |

## 4. Requirements

### 4.1 Permissions and Navigation

| ID | Requirement | Level |
|---|---|---|
| MON-WA-1 | The `/web-users` page and all its mutation endpoints are for administrators only; requests from viewers answer `403`. | MUST |
| MON-WA-2 | The sidebar displays a "Web users" navigation link with a lock icon, visible only to administrators. (The existing "Accounts" entry remains reserved for room clients). | MUST |

### 4.2 Credentials and Storage

| ID | Requirement | Level |
|---|---|---|
| MON-WA-3 | On startup and config load, KAMmon accepts either `password_hash` (PBKDF2-SHA256) or legacy `password` (plain text) for each user. | MUST |
| MON-WA-4 | Whenever an account is added or its password is changed via the UI, KAMmon hashes the password using Go's built-in `crypto/pbkdf2` (SHA-256, 16-byte random salt, 100,000 iterations) and writes `password_hash` to `kammon.yml`. | MUST |
| MON-WA-5 | Passwords and password hashes are never rendered in HTML, sent in API responses, or printed in logs. Input fields for passwords are write-only. | MUST |
| MON-WA-6 | Updates to `kammon.yml` preserve all unrelated configuration sections, keys, and values. | MUST |

### 4.3 Invariants and Validation

| ID | Requirement | Level |
|---|---|---|
| MON-WA-7 | A change that would leave zero administrators is rejected with an error ("At least one administrator is required."). | MUST |
| MON-WA-8 | Usernames must not be empty and must be unique (case-sensitive). | MUST |
| MON-WA-9 | Roles must be either `admin` or `viewer`. | MUST |
| MON-WA-10 | A new user requires a non-empty password. Editing an existing user with a blank password leaves the existing password/hash unchanged. | MUST |

### 4.4 Session Invalidation

| ID | Requirement | Level |
|---|---|---|
| MON-WA-11 | When a user is deleted, renamed, has their role changed, or has their password updated, all active sessions belonging to that user are revoked immediately. The acting admin's session remains active unless they edited their own credentials/role. | MUST |

### 4.5 Demo Mode

| ID | Requirement | Level |
|---|---|---|
| MON-WA-12 | When started with `-demo`, web user mutations update running in-memory authentication state without writing to disk. | MUST |

## 5. Security & Browser Safeguards

- Mutations accept only `POST` or `DELETE` requests carrying the `HX-Request` header and matching `Origin` header (reusing `guard` / `MON-AX-17`, `MON-AX-18`).
- Password comparisons use `subtle.ConstantTimeCompare`.
