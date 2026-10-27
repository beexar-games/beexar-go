package beexar

import "context"

// Wire field names are kept verbatim in the JSON tags — account_id,
// id_provider, round_id_provider, bonus_amount. What you read in the docs, see
// in a request log and type in your handler is then the same string, and the
// SDK carries no name-mapping table that could drift from the contract.

// Route is one of the callback paths, exactly as the contract names them.
type Route string

// The callback routes: the four every wallet serves, and RouteFreespinsFinish,
// which only a wallet that issues freespins does (see FreespinsFinishHandler).
const (
	RouteBalance         Route = "/balance"
	RouteBetWin          Route = "/betwin"
	RouteRollback        Route = "/rollback"
	RouteFinish          Route = "/finish"
	RouteFreespinsFinish Route = "/freespins/finish"
)

// Routes lists every callback route.
var Routes = []Route{RouteBalance, RouteBetWin, RouteRollback, RouteFinish, RouteFreespinsFinish}

// RequestContext carries everything about the request that is not part of the
// parsed body.
type RequestContext struct {
	Route Route
	// RawBody holds the exact bytes the signature was verified against.
	RawBody []byte
	// Headers has lower-cased keys.
	Headers map[string]string
}

// --- /balance --------------------------------------------------------------

// BalanceRequest asks for a player's current balance.
type BalanceRequest struct {
	AccountID string `json:"account_id"`
	Currency  string `json:"currency"`
	GameID    string `json:"game_id"`
	SessionID string `json:"session_id,omitempty"`
}

// BalanceResult answers a BalanceRequest.
type BalanceResult struct {
	Balance Money
}

// --- /betwin ---------------------------------------------------------------

// TransactionType is "bet" or "win".
type TransactionType string

// The two transaction types /betwin carries.
const (
	TransactionBet TransactionType = "bet"
	TransactionWin TransactionType = "win"
)

// BetWinTransaction is one money movement inside a /betwin request.
type BetWinTransaction struct {
	IDProvider string
	Type       TransactionType
	// Amount is always greater than zero — the SDK rejects 0 before your code
	// runs.
	Amount Money
}

// BetWinRequest debits and/or credits a player.
type BetWinRequest struct {
	AccountID string
	Currency  string
	GameID    string
	RoundID   string
	Finished  bool
	// Transactions arrive in request order. Apply them in this order, atomically.
	Transactions []BetWinTransaction
	SessionID    string
}

// BetWinTransactionResult is your answer for one transaction.
type BetWinTransactionResult struct {
	// IDProvider echoes the platform's id back unchanged.
	IDProvider string
	// ID is YOUR transaction id. On a replay, return the one you stored the
	// first time.
	ID string
	// BonusAmount is the bonus balance moved by this transaction. The contract
	// requires the field; the SDK emits "0.00" when you leave it nil.
	BonusAmount *Money
}

// BetWinResult answers a BetWinRequest.
type BetWinResult struct {
	// RoundID is YOUR round id.
	RoundID string
	// Balance after all transactions in this request.
	Balance Money
	// Transactions holds one entry per request transaction, in the same order.
	Transactions []BetWinTransactionResult
}

// --- /rollback -------------------------------------------------------------

// RollbackTransaction reverses one earlier transaction.
type RollbackTransaction struct {
	IDProvider string
	// OriginalIDProvider is the id_provider of the transaction being reversed.
	OriginalIDProvider string
}

// RollbackRequest reverses transactions in a round.
type RollbackRequest struct {
	AccountID string
	Currency  string
	// GameID is required by the contract and always sent by the platform, but
	// the SDK accepts its absence and gives you "" rather than rejecting a
	// request it could have served. Liberal in, strict out.
	GameID string
	// RoundIDProvider — note the name: rollback uses round_id_provider while
	// betwin and finish use round_id.
	RoundIDProvider string
	Finished        bool
	Transactions    []RollbackTransaction
	SessionID       string
}

// RollbackTransactionResult is your answer for one rollback transaction.
type RollbackTransactionResult struct {
	IDProvider string
	// ID is your transaction id, or "" when there was nothing to reverse.
	ID string
}

// RollbackResult answers a RollbackRequest.
type RollbackResult struct {
	Balance      Money
	RoundID      string
	Transactions []RollbackTransactionResult
}

// --- /finish ---------------------------------------------------------------

// FinishRequest closes a round. It carries no game_id.
type FinishRequest struct {
	AccountID string
	Currency  string
	RoundID   string
	SessionID string
}

// FinishResult answers a FinishRequest.
type FinishResult struct {
	Balance Money
}

// --- /freespins/finish -----------------------------------------------------

// FreespinsFinishRequest is the result of a freespins issue: what its
// freespins won in total, for you to credit to the player you issued them to.
type FreespinsFinishRequest struct {
	// IssueID is YOUR id for the issue — the IssueID you sent to
	// Client.IssueFreespins.
	IssueID string
	// Amount is the total won, in the issue's currency at its scale. Zero or
	// more — zero ("0.00" in EUR, "0" in JPY) is a valid result, not an error:
	// the freespins were played and won nothing.
	Amount Money
}

// FreespinsFinishResult answers a FreespinsFinishRequest.
type FreespinsFinishResult struct {
	// Balance after the credit. On a repeat, the one you returned the first
	// time.
	Balance Money
}

// WalletHandler is the four methods you implement against your own ledger.
// Everything else — signature verification, parsing, validation, the Twirp
// error envelope — is the SDK's job.
//
// Return a *WalletError to answer with a specific api_code. Any other error
// becomes an opaque 500 and its message never leaves your process.
//
// A wallet that issues freespins implements FreespinsFinishHandler as well.
type WalletHandler interface {
	Balance(ctx context.Context, req BalanceRequest, rc RequestContext) (BalanceResult, error)
	BetWin(ctx context.Context, req BetWinRequest, rc RequestContext) (BetWinResult, error)
	Rollback(ctx context.Context, req RollbackRequest, rc RequestContext) (RollbackResult, error)
	Finish(ctx context.Context, req FinishRequest, rc RequestContext) (FinishResult, error)
}

// FreespinsFinishHandler is the optional fifth method, for a wallet that
// issues freespins: /freespins/finish. Implement it on the same value you pass
// to NewWalletServer; a handler without it answers that route the way an
// unknown one is answered (api_code 404).
//
// Credit Amount to the player of the issue once per IssueID. The platform
// sends the result once and retries the same body until it gets a 200, so a
// repeat must answer the balance you stored the first time and credit nothing.
type FreespinsFinishHandler interface {
	FreespinsFinish(ctx context.Context, req FreespinsFinishRequest, rc RequestContext) (FreespinsFinishResult, error)
}

// --- launcher --------------------------------------------------------------

// LaunchAccount identifies the player a real-money session is launched for.
type LaunchAccount struct {
	ID           string   `json:"id"`
	Currency     string   `json:"currency"`
	Firstname    string   `json:"firstname,omitempty"`
	Lastname     string   `json:"lastname,omitempty"`
	Nickname     string   `json:"nickname,omitempty"`
	Email        string   `json:"email,omitempty"`
	Country      string   `json:"country,omitempty"`
	DateOfBirth  string   `json:"date_of_birth,omitempty"`
	RegisteredAt string   `json:"registered_at,omitempty"`
	Tags         []string `json:"tags,omitempty"`
}

// LaunchURLs are the pages the game links back to.
type LaunchURLs struct {
	ReturnURL  string `json:"return_url,omitempty"`
	DepositURL string `json:"deposit_url,omitempty"`
}

// LaunchRealRequest launches a real-money session.
type LaunchRealRequest struct {
	Game           string        `json:"game"`
	Account        LaunchAccount `json:"account"`
	Locale         string        `json:"locale,omitempty"`
	IP             string        `json:"ip,omitempty"`
	ClientType     string        `json:"client_type,omitempty"`
	URLs           *LaunchURLs   `json:"urls,omitempty"`
	Jurisdiction   string        `json:"jurisdiction,omitempty"`
	SessionID      string        `json:"session_id,omitempty"`
	SessionPayload string        `json:"session_payload,omitempty"`
}

// LaunchDemoRequest launches a demo session on a virtual balance.
type LaunchDemoRequest struct {
	Game         string      `json:"game"`
	Currency     string      `json:"currency,omitempty"`
	Balance      string      `json:"balance,omitempty"`
	PlayerID     string      `json:"player_id,omitempty"`
	Locale       string      `json:"locale,omitempty"`
	IP           string      `json:"ip,omitempty"`
	ClientType   string      `json:"client_type,omitempty"`
	URLs         *LaunchURLs `json:"urls,omitempty"`
	Jurisdiction string      `json:"jurisdiction,omitempty"`
}

// LaunchResult carries the URL to embed.
type LaunchResult struct {
	LaunchURL string `json:"launch_url"`
}

// GameInfo describes one game in the operator catalogue.
type GameInfo struct {
	Title            string   `json:"title,omitempty"`
	Identifier       string   `json:"identifier,omitempty"`
	Category         string   `json:"category,omitempty"`
	FeatureGroup     string   `json:"feature_group,omitempty"`
	Payout           float64  `json:"payout,omitempty"`
	VolatilityRating string   `json:"volatility_rating,omitempty"`
	HasFreespins     bool     `json:"has_freespins,omitempty"`
	BonusBuy         bool     `json:"bonus_buy,omitempty"`
	DemoAvailable    bool     `json:"demo_available,omitempty"`
	Thumbnail        string   `json:"thumbnail,omitempty"`
	Currencies       []string `json:"currencies,omitempty"`
	// Restrictions carries the country blacklist that applies to this game.
	Restrictions *GameRestrictions `json:"restrictions,omitempty"`
	// Images are every catalog image the game has, one per format, the
	// square tile first: square_640 (640×640), portrait_3x4 (600×800),
	// landscape_16x9 (1280×720), social_1200x630 (1200×630 link-preview
	// card). Empty when the game has none — Thumbnail then carries the
	// platform default.
	Images []GameImage `json:"images,omitempty"`
}

// GameImage is one catalog image of a game: a PNG, with a WebP copy when
// available.
type GameImage struct {
	Type    string `json:"type,omitempty"`
	Width   int    `json:"width,omitempty"`
	Height  int    `json:"height,omitempty"`
	URL     string `json:"url,omitempty"`
	WebPURL string `json:"webp_url,omitempty"`
}

// GameRestrictions groups the per-game availability rules.
type GameRestrictions struct {
	Default *GameRestrictionRule `json:"default,omitempty"`
}

// GameRestrictionRule is one set of country restrictions.
type GameRestrictionRule struct {
	// Blacklist holds ISO country codes the game must not be offered in.
	Blacklist []string `json:"blacklist,omitempty"`
}

// ListGamesQuery filters the catalogue.
type ListGamesQuery struct {
	// Operator defaults to the client's CasinoID.
	Operator string
	// Active, when set, filters by whether the game is enabled.
	Active *bool
}

// --- freespins -------------------------------------------------------------

// FreespinsIssueRequest is Freespins/Issue: FreespinsQuantity free rounds for
// one player, at one bet level, in one or more games. The client adds
// casino_id.
//
// Record the issue in your own ledger BEFORE sending it: the result arrives
// later on /freespins/finish carrying only issue_id and amount, so your record
// is what says whose wallet, in which currency, to credit.
type FreespinsIssueRequest struct {
	// IssueID is YOUR id for this issue, at most 255 characters. Repeating it
	// with the same terms answers like the first call; with other terms it is
	// refused (610).
	IssueID string `json:"issue_id"`
	// Games are game slugs. The quantity is shared by all of them, not given
	// in each.
	Games []string `json:"games"`
	// FreespinsQuantity is at most 10000.
	FreespinsQuantity int64 `json:"freespins_quantity"`
	// BetLevel is a level from Client.BetLevels — the total bet of every free
	// round.
	BetLevel int64 `json:"bet_level"`
	// ValidUntil is when the freespins stop being playable, at most one month
	// ahead, in UTC with a Z: time.Now().UTC().Add(7*24*time.Hour).Format(time.RFC3339).
	ValidUntil string `json:"valid_until"`
	// Account is the player, as in a launch. ID and Currency decide whose
	// freespins these are.
	Account LaunchAccount `json:"account"`
}

// FreespinsCancelRequest is Freespins/Cancel. The client adds casino_id.
type FreespinsCancelRequest struct {
	// IssueID is the issue you issued, or one you are about to: a cancel can
	// come first.
	IssueID string `json:"issue_id"`
}

// BetLevelsRequest filters Bet/Levels. Unknown values are ignored; leave one
// empty for all of them.
type BetLevelsRequest struct {
	// Games are game slugs.
	Games []string `json:"games,omitempty"`
	// Currencies are currency codes.
	Currencies []string `json:"currencies,omitempty"`
}

// BetLevelData is the games that share the same bet levels in every currency.
// Each game appears once.
type BetLevelData struct {
	Games      []string            `json:"games"`
	Currencies []CurrencyBetLevels `json:"currencies"`
}

// CurrencyBetLevels is one currency's bet levels: 1..N without gaps, amounts
// strictly ascending.
type CurrencyBetLevels struct {
	Currency  string     `json:"currency"`
	BetLevels []BetLevel `json:"bet_levels"`
}

// BetLevel is what one bet level costs per round in one currency.
type BetLevel struct {
	BetLevel int64 `json:"bet_level"`
	// Amount is the TOTAL bet of one round, a decimal string in the currency's
	// main unit — ParseMoney(level.Amount) for arithmetic.
	Amount string `json:"amount"`
}
