# Phone backup: opt-in client-side encryption (design note, WP2-8)

Status: research and design only, nothing built. Decisions it rests on:
plan D6 and §7.3 (2026-09-24-mobile-app-plan.md): off by default for
every category, the server never sees the key, losing the passphrase
loses those backups. Wire flag already reserved: tus metadata
`encrypted=1` and `PhoneExport.encrypted` (device API §5, §7).

## What gets encrypted

Per category, chosen in Backup settings, fixed after the category's first
encrypted run (switching means "start this category fresh", like a new
location). Every file and export of that category is stored encrypted:
photos, folders, APKs, contacts .vcf, calendars .ics, the SMS and call
log XML, the app list and settings JSON.

Not encrypted: paths/names, sizes, mtimes, taken_at (the server needs
them for browse, retention and "deleted on the phone"). Path names can be
sensitive ("DCIM/Divorce lawyer"); hiding them would make the server
unable to browse or prune, so it is out of scope, stated in the screen.

## Keys

- Recovery passphrase: 12 words from a fixed 2048-word list (BIP-39
  English, 128 bits + checksum), generated on the phone, shown once,
  "I saved it" required, re-shown only after the screen lock (local_auth).
- Key encryption key = Argon2id(passphrase, salt 16 bytes, m = 64 MiB,
  t = 3, p = 1) -> 32 bytes. Parameters stored with the salt so they can
  be raised later.
- A random 32-byte data key encrypts the files; it is stored on the
  server wrapped (AES-256-GCM under the key encryption key) in the
  device's `phone-settings`, so a new phone needs only the passphrase.
  Changing the passphrase rewraps the data key; no file is rewritten.
- On the phone the data key is kept in flutter_secure_storage (Keystore
  backed), so scheduled jobs run without the passphrase.

## File format (versioned, chunked)

```
magic "NVE1" | version u8 = 1 | chunk_log2 u8 = 20 (1 MiB) | nonce_prefix 7 bytes
then per chunk i: AES-256-GCM(data key, nonce = nonce_prefix || i (u32 BE) || last (u8),
                              aad = header) -> ciphertext || 16-byte tag
```

The STREAM construction (per-chunk nonce with a last-chunk flag) stops
reordering, truncation and chunk swaps between files; the header as AAD
pins the parameters. 1 MiB chunks fit the tus upload chunks (8 MiB) and
let a restore check and write as it downloads. Overhead: 13 bytes plus
16 per MiB.

## What changes on the server side

- Dedup and `have`: the server matches by SHA-256 of what it stores. With
  random nonces the same photo encrypts differently each time, so the
  phone must remember plaintext hash -> ciphertext hash for what it sent
  (the local manifest, pb_manifest.dart, gains a fourth column) and check
  with the ciphertext hash. Upside: no deterministic encryption, which
  would leak "these two files are the same" to the server.
- Exclusions (`/excluded`) match plaintext hashes the server can't see:
  for encrypted categories the phone applies them itself.
- SMS / call log: the server can't parse an encrypted export, so it can't
  record item keys or merge `exports/full`. The phone keeps its own list
  of sent keys (already anticipated in device API §7) and a restore
  downloads every export of the category and merges them on the phone
  (pb_messages already writes the format; the MMS restore reads it).
- Browse in the web UI shows names and sizes with a lock, no thumbnails,
  no download (or a download of the ciphertext with a note).
- Verify (`POST /verify`) still works: it checks the stored ciphertext's
  hash.

## Libraries (ladder)

- AES-GCM: Android has it (javax.crypto, `AES/GCM/NoPadding`); the
  installed `crypto` package is hashes only. Options: (a) native javax.crypto through the phone_backup channel, which
  is already how bytes are read (no new dependency, fast, hardware AES);
  (b) the `cryptography` package (pure Dart + platform acceleration).
  Recommended: (a) for the bulk file work, since the bytes already pass
  through Kotlin.
- Argon2id: not in the Android framework (BouncyCastle in the platform is
  hidden). Options: `cryptography`'s Argon2id (pure Dart, ~1-2 s at
  64 MiB on a mid phone, once per passphrase entry) or a small native
  library. Recommended: `cryptography` for Argon2id only, or skip Argon2
  and use PBKDF2-HMAC-SHA256 (framework, 600k iterations) if adding a
  dependency is refused; Argon2id is the stated plan decision, so that
  trade is the owner's call.
- Known-answer tests: RFC 9106 Argon2id vectors, NIST GCM vectors, and a
  fixed-key round trip of a 3-chunk file (truncation and chunk swap must
  fail).

## Effort and order

About 3 ed as planned (WP2-8), plus ~1 ed for the phone-side SMS merge and
the manifest column. Do after the restore paths are stable, since every
restore path (media, folders, exports, SMS/MMS, APKs) has to decrypt.

## Open questions for the owner

1. Argon2id via a new dependency, or PBKDF2 from the framework?
2. Is hiding file names wanted at the cost of server-side browse/pruning?
3. Should the wrapped data key live on the server (new phone needs only
   the passphrase) or only in the user's hands (a key file to keep)?
