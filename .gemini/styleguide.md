# Chirp Code Review Style Guide

Review pull requests against these hard invariants of the Chirp codebase:

1. **Consent & product model (`docs/product-model.md`)**: Every remote support session requires explicit interactive consent on the owner's own machine. Flag any flag, config option, code path, or default that allows unattended access, pre-authorisation, or bypassing the consent prompt.
2. **Blind lookup & PAKE secrets (`client/pake.go`, `session/lookup.go`)**: The 4-digit secret part of a 10-digit code (or `.NNNN` on a link) must never leave the two Chirp endpoints — never send it to the relay/lookup server or log it. Also never log raw codes, link tokens, `peer_id` credentials, or TURN credentials.
3. **Licensing & dependency boundary (`LICENSING.md`, `scripts/gen-third-party-notices.sh`)**:
   - Every new `.go` file must begin with:
     ```go
     // SPDX-License-Identifier: AGPL-3.0-only
     // Copyright (C) 2026 Fabrintek Engenharia Digital Ltda
     ```
   - Only permissive dependencies (MIT, BSD, Apache-2.0, ISC) are allowed; reject copyleft (GPL/LGPL/AGPL) or MPL-2.0 dependencies.
   - Never import `github.com/barahn/remotekit/tunnel` (it pulls in yamux / MPL-2.0 and the standing-agent model).
4. **CGO boundary**: Only `cmd/chirp` (and packages under it) may import Fyne or require cgo. `session`, `signalling`, `client`, `node`, `status`, `cmd/chirp-relay`, and `cmd/chirp-status` must continue building with `CGO_ENABLED=0`.
5. **Workflows & supply chain (`docs/devsecops.md`)**: Every GitHub Action must be pinned to a full 40-character commit SHA with a trailing version comment; `actions/checkout` must set `persist-credentials: false`; jobs must request minimal `permissions`; `actions/setup-go` in push/tag/release workflows must set `cache: false`.
6. **Generated files**: Any change to tracked files, packages, direct requirements, or test names requires refreshing `HANDOFF.md` with `./scripts/gen-handoff.sh HANDOFF.md`. Any change to Windows resources or icons requires refreshing `cmd/chirp/rsrc_windows_amd64.syso` with `./scripts/gen-winres.sh`.
