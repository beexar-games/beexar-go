package beexar

import "context"

// Wire field names are kept verbatim in the JSON tags — account_id,
// id_provider, round_id_provider, bonus_amount. What you read in the docs, see
// in a request log and type in your handler is then the same string, and the
// SDK carries no name-mapping table that could drift from the contract.

// Route is one of the four callback paths, exactly as the contract names them.
type Route string

// The four callback routes.
const (
	RouteBalance  Route = "/balance"
	RouteBetWin   Route = "/betwin"
	RouteRollback Route = "/rollback"
	RouteFinish   Route = "/finish"
)

// Routes lists every callback route.
var Routes = []Route{RouteBalance, RouteBetWin, RouteRollback, RouteFinish}

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

// WalletHandler is the four methods you implement against your own ledger.
// Everything else — signature verification, parsing, validation, the Twirp
// error envelope — is the SDK's job.
//
// Return a *WalletError to answer with a specific api_code. Any other error
// becomes an opaque 500 and its message never leaves your process.
type WalletHandler interface {
	Balance(ctx context.Context, req BalanceRequest, rc RequestContext) (BalanceResult, error)
	BetWin(ctx context.Context, req BetWinRequest, rc RequestContext) (BetWinResult, error)
	Rollback(ctx context.Context, req RollbackRequest, rc RequestContext) (RollbackResult, error)
	Finish(ctx context.Context, req FinishRequest, rc RequestContext) (FinishResult, error)
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
