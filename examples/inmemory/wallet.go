// Package inmemory is a complete, correct Beexar wallet held in memory.
//
// Not durable, not shared between processes, not your ledger. It exists to show
// the three rules that decide whether an integration is correct, in the order
// they have to happen:
//
//  1. Dedupe on id_provider FIRST. A repeat must return the transaction id and
//     the balance you STORED the first time — not today's balance, not a fresh
//     id. The platform retries, and a retry must be invisible.
//  2. THEN check the tombstone. A rollback can arrive before the bet it
//     reverses. When that bet finally shows up it must be refused with api_code
//     409, not applied.
//  3. THEN apply, atomically. Either every transaction in the request lands or
//     none does, and an insufficient-funds answer reports the balance as it was
//     BEFORE the batch — because nothing moved.
//
// The single mutex below stands in for your BEGIN/COMMIT. In a real wallet the
// dedupe read, the tombstone read and the balance update must all be in one
// database transaction. That is why the SDK does not offer to do idempotency
// for you: it cannot join your transaction, so it could only pretend.
package inmemory

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"sync"

	beexar "github.com/beexar-games/beexar-go"
)

// StoredTransaction is one row of the transactions table.
type StoredTransaction struct {
	// ID is your transaction id. Empty when a rollback found nothing to reverse.
	ID string `json:"id"`
	// BalanceAfter is the balance you reported after applying it.
	BalanceAfter string `json:"balance_after"`
	// Type and Amount are recorded only for transactions a rollback can reverse.
	Type   string `json:"type,omitempty"`
	Amount string `json:"amount,omitempty"`
}

// StoredAccount is one row of the accounts table.
type StoredAccount struct {
	Currency string `json:"currency"`
	Balance  string `json:"balance"`
	// BetLimited makes this account answer api_code 105 instead of betting.
	BetLimited bool `json:"bet_limited,omitempty"`
}

// LedgerState is the whole wallet, in the shape the conformance fixtures use.
type LedgerState struct {
	Accounts     map[string]StoredAccount     `json:"accounts"`
	Transactions map[string]StoredTransaction `json:"transactions"`
	Tombstones   []string                     `json:"tombstones"`
}

type account struct {
	currency   string
	balance    beexar.Money
	betLimited bool
}

// Wallet implements beexar.WalletHandler over the maps above.
type Wallet struct {
	mu           sync.Mutex
	accounts     map[string]*account
	transactions map[string]StoredTransaction
	tombstones   map[string]bool
	rounds       map[string]string
	nextTx       int
	nextRound    int
}

var opTxID = regexp.MustCompile(`^op-tx-(\d+)$`)

// New builds a wallet from a starting state.
func New(state LedgerState) *Wallet {
	w := &Wallet{
		accounts:     map[string]*account{},
		transactions: map[string]StoredTransaction{},
		tombstones:   map[string]bool{},
		rounds:       map[string]string{},
		nextRound:    1,
	}
	for id, a := range state.Accounts {
		w.accounts[id] = &account{
			currency:   a.Currency,
			balance:    beexar.MustParseMoney(a.Balance),
			betLimited: a.BetLimited,
		}
	}
	max := 0
	for id, t := range state.Transactions {
		w.transactions[id] = t
		if m := opTxID.FindStringSubmatch(t.ID); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > max {
				max = n
			}
		}
	}
	for _, id := range state.Tombstones {
		w.tombstones[id] = true
	}
	// Continue the id sequence rather than restarting it, so ids stay unique
	// across a restored state.
	w.nextTx = max + 1
	return w
}

// Snapshot returns the current state in the same shape New takes.
func (w *Wallet) Snapshot() LedgerState {
	w.mu.Lock()
	defer w.mu.Unlock()

	out := LedgerState{
		Accounts:     map[string]StoredAccount{},
		Transactions: map[string]StoredTransaction{},
		Tombstones:   []string{},
	}
	for id, a := range w.accounts {
		out.Accounts[id] = StoredAccount{Currency: a.currency, Balance: a.balance.String(), BetLimited: a.betLimited}
	}
	for id, t := range w.transactions {
		out.Transactions[id] = t
	}
	for id := range w.tombstones {
		out.Tombstones = append(out.Tombstones, id)
	}
	return out
}

// Balance answers the current balance.
func (w *Wallet) Balance(_ context.Context, req beexar.BalanceRequest, _ beexar.RequestContext) (beexar.BalanceResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	a, err := w.account(req.AccountID)
	if err != nil {
		return beexar.BalanceResult{}, err
	}
	return beexar.BalanceResult{Balance: a.balance}, nil
}

// BetWin debits and credits atomically.
func (w *Wallet) BetWin(_ context.Context, req beexar.BetWinRequest, _ beexar.RequestContext) (beexar.BetWinResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	a, err := w.account(req.AccountID)
	if err != nil {
		return beexar.BetWinResult{}, err
	}
	balanceBefore := a.balance

	// Everything is computed against a working copy first. Nothing touches the
	// stored state until the whole batch is known to succeed.
	running := balanceBefore
	var replayed *beexar.Money
	type pending struct {
		idProvider string
		stored     StoredTransaction
	}
	var applied []pending
	results := make([]beexar.BetWinTransactionResult, 0, len(req.Transactions))

	for _, t := range req.Transactions {
		if seen, ok := w.transactions[t.IDProvider]; ok {
			// 1. Replay: answer with what we stored, and change nothing.
			results = append(results, beexar.BetWinTransactionResult{IDProvider: t.IDProvider, ID: seen.ID})
			m := beexar.MustParseMoney(seen.BalanceAfter)
			replayed = &m
			continue
		}
		if w.tombstones[t.IDProvider] {
			// 2. Its rollback got here first.
			return beexar.BetWinResult{}, beexar.ErrAlreadyRolledBack()
		}
		if t.Type == beexar.TransactionBet && a.betLimited {
			return beexar.BetWinResult{}, beexar.ErrBetLimitReached(balanceBefore)
		}

		// 3. Apply on the working copy.
		if t.Type == beexar.TransactionBet {
			after := running.Sub(t.Amount)
			if after.IsNegative() {
				// Report the balance as it stands — nothing in this batch was applied.
				return beexar.BetWinResult{}, beexar.ErrInsufficientFunds(balanceBefore)
			}
			running = after
		} else {
			running = running.Add(t.Amount)
		}

		id := w.allocateTxID()
		applied = append(applied, pending{
			idProvider: t.IDProvider,
			stored: StoredTransaction{
				ID:           id,
				BalanceAfter: running.String(),
				Type:         string(t.Type),
				Amount:       t.Amount.String(),
			},
		})
		results = append(results, beexar.BetWinTransactionResult{IDProvider: t.IDProvider, ID: id})
	}

	// Commit only if something was actually applied. A request made entirely of
	// repeats must leave the ledger exactly as it found it.
	balance := running
	if len(applied) > 0 {
		for _, p := range applied {
			w.transactions[p.idProvider] = p.stored
		}
		a.balance = running
	} else if replayed != nil {
		balance = *replayed
	}

	return beexar.BetWinResult{RoundID: w.roundIDFor(req.RoundID), Balance: balance, Transactions: results}, nil
}

// Rollback reverses transactions and records tombstones.
func (w *Wallet) Rollback(_ context.Context, req beexar.RollbackRequest, _ beexar.RequestContext) (beexar.RollbackResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	a, err := w.account(req.AccountID)
	if err != nil {
		return beexar.RollbackResult{}, err
	}

	running := a.balance
	var replayed *beexar.Money
	changed := false
	results := make([]beexar.RollbackTransactionResult, 0, len(req.Transactions))

	for _, t := range req.Transactions {
		if seen, ok := w.transactions[t.IDProvider]; ok {
			results = append(results, beexar.RollbackTransactionResult{IDProvider: t.IDProvider, ID: seen.ID})
			m := beexar.MustParseMoney(seen.BalanceAfter)
			replayed = &m
			continue
		}

		// The tombstone goes down whether or not the original ever arrived.
		// That is what makes an out-of-order rollback safe: when the bet turns
		// up, the tombstone is already there to refuse it.
		w.tombstones[t.OriginalIDProvider] = true
		changed = true

		id := ""
		if original, ok := w.transactions[t.OriginalIDProvider]; ok && original.Type != "" && original.Amount != "" {
			amount := beexar.MustParseMoney(original.Amount)
			if original.Type == string(beexar.TransactionBet) {
				running = running.Add(amount)
			} else {
				// Reversing a win can only take back what is there. Clamping at
				// zero keeps a player from going negative because of our
				// bookkeeping.
				running = running.Sub(amount).ClampToZero()
			}
			id = w.allocateTxID()
		}

		w.transactions[t.IDProvider] = StoredTransaction{ID: id, BalanceAfter: running.String()}
		results = append(results, beexar.RollbackTransactionResult{IDProvider: t.IDProvider, ID: id})
	}

	balance := running
	if changed {
		a.balance = running
	} else if replayed != nil {
		balance = *replayed
	}

	return beexar.RollbackResult{Balance: balance, RoundID: w.roundIDFor(req.RoundIDProvider), Transactions: results}, nil
}

// Finish acknowledges the end of a round. The money already moved through
// BetWin, so answering the current balance and staying idempotent is the job.
func (w *Wallet) Finish(_ context.Context, req beexar.FinishRequest, _ beexar.RequestContext) (beexar.FinishResult, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	a, err := w.account(req.AccountID)
	if err != nil {
		return beexar.FinishResult{}, err
	}
	return beexar.FinishResult{Balance: a.balance}, nil
}

func (w *Wallet) account(id string) (*account, error) {
	a, ok := w.accounts[id]
	if !ok {
		return nil, beexar.ErrInvalidPlayer(fmt.Sprintf("unknown account %s", id))
	}
	return a, nil
}

func (w *Wallet) allocateTxID() string {
	id := fmt.Sprintf("op-tx-%d", w.nextTx)
	w.nextTx++
	return id
}

func (w *Wallet) roundIDFor(providerRoundID string) string {
	if own, ok := w.rounds[providerRoundID]; ok {
		return own
	}
	own := fmt.Sprintf("op-round-%d", w.nextRound)
	w.nextRound++
	w.rounds[providerRoundID] = own
	return own
}
