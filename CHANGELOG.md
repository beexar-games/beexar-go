# Changelog

All notable changes to `github.com/beexar-games/beexar-go`. The version is shared across the Beexar SDKs
for Node, PHP, Go and Python — the same number always means the same contract
snapshot.

## 1.0.1 — 2026-09-18

No change to the code you consume. The release exists to move publishing onto
npm's and PyPI's trusted publishing, so no long-lived registry token is stored
anywhere any more.

## 1.0.0 — 2026-09-18

First public release.

- `Client` — LaunchReal, LaunchDemo, ListGames, signed with HMAC-SHA256 over the exact request bytes.
- `WalletServer — the four seamless-wallet callbacks as one pure Dispatch(ctx, route, rawBody, signature), plus NewHandler() for net/http`.
- Money — decimal amounts that marshal to JSON strings and have no float constructor.
- The full api_code registry, with the balance a required argument for codes
  100, 105 and 106.
- Types tracked against the published OpenAPI documents: a contract test fails
  if a property the spec declares never reaches your handler.
- Zero dependencies: standard library only, in the SDK and in its tests.
