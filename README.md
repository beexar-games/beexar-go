# beexar-go

Beexar operator SDK for Go. Launch game sessions, and serve the four
seamless-wallet callbacks the platform calls during play.

**Zero dependencies** — standard library only, in the SDK and in its tests.
Go 1.22+.

```bash
go get github.com/beexar-games/beexar-go
```

Full docs: **https://docs.beexar.com** · OpenAPI: **https://docs.beexar.com/api-reference/**

---

## The integration in one picture

There are two halves, and the second one is the work.

```
you  ──  POST /api/v1/softswiss/launcher/real  ──▶  Beexar     (Client)
                                                      │
player plays                                          │
                                                      ▼
your wallet  ◀──  POST /balance /betwin /rollback /finish  ──  Beexar   (WalletServer)
```

## Half 1 — launching a game

```go
client, err := beexar.NewClient(os.Getenv("BEEXAR_CASINO_ID"), os.Getenv("BEEXAR_API_SECRET"))
if err != nil {
    return err
}

res, err := client.LaunchReal(ctx, beexar.LaunchRealRequest{
    Game:    "dice",
    Account: beexar.LaunchAccount{ID: "player_123", Currency: "EUR"},
    Locale:  "en",
})
// res.LaunchURL goes in an iframe
```

`LaunchDemo` does the same on a virtual balance and makes no wallet calls.
`ListGames` returns the catalogue enabled for you.

## Half 2 — serving the wallet

Implement four methods against your ledger. Everything else — signature
verification, parsing, validation, the error envelope — is handled.

```go
type Wallet struct{ db *sql.DB }

func (w *Wallet) BetWin(ctx context.Context, req beexar.BetWinRequest, _ beexar.RequestContext) (beexar.BetWinResult, error) {
    // Everything below must be in ONE database transaction. See "The boundary".
    var results []beexar.BetWinTransactionResult
    for _, t := range req.Transactions {
        if seen, ok := w.lookup(ctx, t.IDProvider); ok {
            results = append(results, beexar.BetWinTransactionResult{IDProvider: t.IDProvider, ID: seen.ID})
            continue
        }
        if w.isRolledBack(ctx, t.IDProvider) {
            return beexar.BetWinResult{}, beexar.ErrAlreadyRolledBack()
        }
        if t.Type == beexar.TransactionBet && w.balance(ctx).Cmp(t.Amount) < 0 {
            return beexar.BetWinResult{}, beexar.ErrInsufficientFunds(w.balance(ctx))
        }
        id := w.apply(ctx, t)
        results = append(results, beexar.BetWinTransactionResult{IDProvider: t.IDProvider, ID: id})
    }
    return beexar.BetWinResult{RoundID: w.roundID(req.RoundID), Balance: w.balance(ctx), Transactions: results}, nil
}

// … Balance, Rollback, Finish

server, _ := beexar.NewWalletServer(&Wallet{db}, os.Getenv("BEEXAR_API_SECRET"))
mux.Handle("/wallet/", server.NewHandler())
```

Then point the four callback URLs in the backoffice at
`https://your-host/wallet/{balance,betwin,rollback,finish}` and run the
[Integration Test Game](https://docs.beexar.com/guides/testing/) — 29 scenarios
against your implementation.

A complete, correct wallet you can read in one sitting:
[`examples/inmemory`](./examples/inmemory/wallet.go). A runnable server:
[`examples/server`](./examples/server/main.go).

## The raw body

The signature is an HMAC over the **exact bytes** of the request. `NewHandler`
reads them once and hands the same slice to `Dispatch`. If you wire the
callbacks yourself, read the body **before** anything decodes it, and pass those
bytes:

```go
body, _ := io.ReadAll(io.LimitReader(r.Body, beexar.MaxBodyBytes+1))
out := server.Dispatch(r.Context(), beexar.RouteBetWin, body, r.Header.Get(beexar.SignatureHeader), nil)
```

Decoding and re-encoding the JSON changes key order, escaping and number
rendering — the bytes that were signed are then gone for good. When a signature
fails and the SDK can tell why, `beexar.WithWarning` gives you the reason in
plain words.

## Money

Amounts and balances are decimal strings in the currency's main unit — `"0.90"`
is ninety cents. `beexar.Money` has no float constructor and marshals to a JSON
string, so an amount can never become a `float64` on the way past.

```go
beexar.MustParseMoney("100.00").Sub(beexar.MustParseMoney("0.30")).String() // "99.70"
beexar.MoneyFromMinorUnits(big.NewInt(9970), 2)                             // "99.70" from an integer ledger
```

`"1E2000000000"` and anything past 16 decimals is rejected on shape, before any
arithmetic touches it.

## Errors

Two HTTP statuses exist on this contract, 400 and 500, and the meaning lives in
`meta.api_code`. Return a `*WalletError` and the envelope is built for you:

```go
return beexar.ErrInsufficientFunds(currentBalance) // 400 / api_code 100
return beexar.ErrAlreadyRolledBack()               // 400 / api_code 409
return beexar.ErrInvalidPlayer("")                 // 400 / api_code 101
```

Codes 100, 105 and 106 **must** carry the player's balance, so those
constructors take it as a required parameter — there is no way to build one
without it. Any other error becomes an opaque 500 and its message never leaves
your process.

## The boundary

The SDK does **not** do idempotency or tombstones for you, and it will not
pretend to. Both have to happen in the same database transaction as the balance
update, and no library can join your transaction. What you must do:

1. Store every `id_provider` with a unique index, and check it **inside** the
   transaction that moves the money.
2. Store the response you returned — a repeat must return the id and balance you
   gave the first time, not today's.
3. On rollback, record a tombstone for `OriginalIDProvider` **whether or not**
   the original exists. Out-of-order delivery is normal; a later `/betwin` for a
   tombstoned id must be refused with `ErrAlreadyRolledBack()`.

Your handler has **15 seconds**. The platform retries 5xx and timeouts for up to
30 seconds with the same `id_provider`; it does not retry insufficient funds,
bet limits, bad requests or signature failures.

## Types come from the spec

The types in this package track the published OpenAPI documents (`openapi/` in
this repo). `contract_test.go` reads a normalised description of those specs and
fails if a field the contract declares is missing here — a change to the
contract breaks the build rather than reaching you as a dropped value.

## License

MIT
