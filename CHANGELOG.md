# Changelog

All notable changes to `github.com/beexar-games/beexar-go`. The version is shared across the Beexar SDKs
for Node, PHP, Go and Python — the same number always means the same contract
snapshot.

## 1.2.0 — 2026-10-23

Operator freespins, the SoftSwiss Freespins API one to one. Additions only: a
`WalletHandler` that does not issue freespins compiles and runs unchanged.

- `Client.IssueFreespins`, `Client.CancelFreespins` and `Client.BetLevels` —
  Freespins/Issue, Freespins/Cancel and Bet/Levels, signed over the exact bytes
  like the launch calls. Their types: `FreespinsIssueRequest`,
  `FreespinsCancelRequest`, `BetLevelsRequest`, `BetLevelData`,
  `CurrencyBetLevels`, `BetLevel`.
- `/freespins/finish`, the fifth callback: implement the optional
  `FreespinsFinishHandler` on your handler to receive a `FreespinsFinishRequest`
  (`Amount` as `Money`, zero allowed). Without it the route answers like an
  unknown one.
- `ErrInvalidFreespinsIssue` (api_code 610) and `ErrFreespinsStateTransition`
  (605); the `APICodeFreespins*` constants.
- `APICodeFeatureNotSupported` (608): `IssueFreespins` answers it for a game
  that plays no freespins, and `APICodeGameNotAvailable` (405) for a game your
  casino does not have; 601 is left for a missing bet level. 406 reads "This
  feature is not enabled for your casino" (the SoftSwiss registry of
  2026-10-25).
- `RouteFreespinsFinish`, also in `Routes`.
- `RouteFromPath` matches the longest route first. `/freespins/finish` ends with
  `/finish` too, and must not reach the round's finish handler.
- The `casino_id` merge keeps request numbers exact instead of passing them
  through a `float64`.
- The reference wallet credits a freespins result once per issue id, and the
  conformance fixtures (version 2) cover it.
- Conformance fixtures version 3 add `0033-signature-non-ascii`: a malformed
  `X-REQUEST-SIGN` of the right length but not hex is refused like any bad
  signature (HTTP 400, api_code 403). `Verify` already did; the case and
  `TestVerifyRefusesMalformedSignatures` pin it.
- The game catalog no longer lists the built-in slots game, removed on
  2026-10-24: `Category` is `dice`, `plinko`, `crash` or `rgs`. `HasFreespins`
  reports a game's own in-game free spins, not operator freespins; no game
  sets it today.

## 1.1.0 — 2026-10-04

`go 1.26`, up from 1.22: Go supports its last two releases, 1.27 and 1.26. No
change to the API.

## 1.0.2 — 2026-09-18

No change to the code you consume. 1.0.1 reached PyPI, Packagist and the Go
proxy but not npm: the publish workflow pinned `npm@latest`, which had moved to
a major that refuses to run on the Node version the workflow used.

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
