package beexar

import "fmt"

// Validation of the wire shapes, and the conversion to the ergonomic types the
// handler sees. Everything here runs BEFORE the handler, so a handler never has
// to check a currency pattern or wonder whether an amount is a number.

func requireString(value, field string) (string, error) {
	if value == "" {
		return "", ErrBadRequest(field + ": required, must be a non-empty string")
	}
	return value, nil
}

func requireCurrency(value string) (string, error) {
	if !ValidCurrency(value) {
		return "", ErrBadRequest("currency: must match " + CurrencyPattern)
	}
	return value, nil
}

func parseAmount(raw, path string) (Money, error) {
	m, err := ParseMoney(raw)
	if err != nil {
		return Money{}, ErrBadRequest(path + ": not a valid decimal amount")
	}
	if !m.IsPositive() {
		return Money{}, ErrBadRequest(path + ": must be greater than zero")
	}
	return m, nil
}

func (w wireBalanceRequest) toRequest() (BalanceRequest, error) {
	accountID, err := requireString(w.AccountID, "account_id")
	if err != nil {
		return BalanceRequest{}, err
	}
	currency, err := requireCurrency(w.Currency)
	if err != nil {
		return BalanceRequest{}, err
	}
	gameID, err := requireString(w.GameID, "game_id")
	if err != nil {
		return BalanceRequest{}, err
	}
	return BalanceRequest{AccountID: accountID, Currency: currency, GameID: gameID, SessionID: w.SessionID}, nil
}

func (w wireBetWinRequest) toRequest() (BetWinRequest, error) {
	if len(w.Transactions) == 0 {
		return BetWinRequest{}, ErrBadRequest("transactions: required, must be a non-empty array")
	}
	txs := make([]BetWinTransaction, 0, len(w.Transactions))
	for i, t := range w.Transactions {
		idProvider, err := requireString(t.IDProvider, fmt.Sprintf("transactions[%d].id_provider", i))
		if err != nil {
			return BetWinRequest{}, err
		}
		if t.Type != string(TransactionBet) && t.Type != string(TransactionWin) {
			return BetWinRequest{}, ErrBadRequest(fmt.Sprintf("transactions[%d].type: must be \"bet\" or \"win\"", i))
		}
		amount, err := parseAmount(t.Amount, fmt.Sprintf("transactions[%d].amount", i))
		if err != nil {
			return BetWinRequest{}, err
		}
		txs = append(txs, BetWinTransaction{IDProvider: idProvider, Type: TransactionType(t.Type), Amount: amount})
	}

	accountID, err := requireString(w.AccountID, "account_id")
	if err != nil {
		return BetWinRequest{}, err
	}
	currency, err := requireCurrency(w.Currency)
	if err != nil {
		return BetWinRequest{}, err
	}
	gameID, err := requireString(w.GameID, "game_id")
	if err != nil {
		return BetWinRequest{}, err
	}
	roundID, err := requireString(w.RoundID, "round_id")
	if err != nil {
		return BetWinRequest{}, err
	}
	return BetWinRequest{
		AccountID:    accountID,
		Currency:     currency,
		GameID:       gameID,
		RoundID:      roundID,
		Finished:     w.Finished,
		Transactions: txs,
		SessionID:    w.SessionID,
	}, nil
}

func (w wireRollbackRequest) toRequest() (RollbackRequest, error) {
	if len(w.Transactions) == 0 {
		return RollbackRequest{}, ErrBadRequest("transactions: required, must be a non-empty array")
	}
	txs := make([]RollbackTransaction, 0, len(w.Transactions))
	for i, t := range w.Transactions {
		idProvider, err := requireString(t.IDProvider, fmt.Sprintf("transactions[%d].id_provider", i))
		if err != nil {
			return RollbackRequest{}, err
		}
		if t.Type != "rollback" {
			return RollbackRequest{}, ErrBadRequest(fmt.Sprintf("transactions[%d].type: must be \"rollback\"", i))
		}
		original, err := requireString(t.OriginalIDProvider, fmt.Sprintf("transactions[%d].original_id_provider", i))
		if err != nil {
			return RollbackRequest{}, err
		}
		txs = append(txs, RollbackTransaction{IDProvider: idProvider, OriginalIDProvider: original})
	}

	accountID, err := requireString(w.AccountID, "account_id")
	if err != nil {
		return RollbackRequest{}, err
	}
	currency, err := requireCurrency(w.Currency)
	if err != nil {
		return RollbackRequest{}, err
	}
	roundIDProvider, err := requireString(w.RoundIDProvider, "round_id_provider")
	if err != nil {
		return RollbackRequest{}, err
	}
	return RollbackRequest{
		AccountID: accountID,
		Currency:  currency,
		// Liberal: the contract requires game_id and the platform always sends
		// it, but refusing a request we could serve helps nobody.
		GameID:          w.GameID,
		RoundIDProvider: roundIDProvider,
		Finished:        w.Finished,
		Transactions:    txs,
		SessionID:       w.SessionID,
	}, nil
}

func (w wireFinishRequest) toRequest() (FinishRequest, error) {
	accountID, err := requireString(w.AccountID, "account_id")
	if err != nil {
		return FinishRequest{}, err
	}
	currency, err := requireCurrency(w.Currency)
	if err != nil {
		return FinishRequest{}, err
	}
	roundID, err := requireString(w.RoundID, "round_id")
	if err != nil {
		return FinishRequest{}, err
	}
	return FinishRequest{AccountID: accountID, Currency: currency, RoundID: roundID, SessionID: w.SessionID}, nil
}
