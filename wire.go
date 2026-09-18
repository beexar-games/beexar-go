package beexar

// The JSON shapes exactly as they appear on the wire. They are unexported and
// exist only to be decoded into, then converted to the ergonomic types the
// handler sees — which is where "amount" stops being a string and becomes Money.

type wireBalanceRequest struct {
	AccountID string `json:"account_id"`
	Currency  string `json:"currency"`
	GameID    string `json:"game_id"`
	SessionID string `json:"session_id"`
}

type wireBetWinTransaction struct {
	IDProvider string `json:"id_provider"`
	Type       string `json:"type"`
	Amount     string `json:"amount"`
}

type wireBetWinRequest struct {
	AccountID    string                  `json:"account_id"`
	Currency     string                  `json:"currency"`
	GameID       string                  `json:"game_id"`
	RoundID      string                  `json:"round_id"`
	Finished     bool                    `json:"finished"`
	Transactions []wireBetWinTransaction `json:"transactions"`
	SessionID    string                  `json:"session_id"`
}

type wireRollbackTransaction struct {
	IDProvider         string `json:"id_provider"`
	Type               string `json:"type"`
	OriginalIDProvider string `json:"original_id_provider"`
}

type wireRollbackRequest struct {
	AccountID       string                    `json:"account_id"`
	Currency        string                    `json:"currency"`
	GameID          string                    `json:"game_id"`
	RoundIDProvider string                    `json:"round_id_provider"`
	Finished        bool                      `json:"finished"`
	Transactions    []wireRollbackTransaction `json:"transactions"`
	SessionID       string                    `json:"session_id"`
}

type wireFinishRequest struct {
	AccountID string `json:"account_id"`
	Currency  string `json:"currency"`
	RoundID   string `json:"round_id"`
	SessionID string `json:"session_id"`
}

type wireBalanceResponse struct {
	Balance string `json:"balance"`
}

type wireBetWinResponseTransaction struct {
	IDProvider  string `json:"id_provider"`
	ID          string `json:"id"`
	BonusAmount string `json:"bonus_amount"`
}

type wireBetWinResponse struct {
	RoundID      string                          `json:"round_id"`
	Transactions []wireBetWinResponseTransaction `json:"transactions"`
	Balance      string                          `json:"balance"`
}

type wireRollbackResponseTransaction struct {
	IDProvider string `json:"id_provider"`
	ID         string `json:"id"`
}

type wireRollbackResponse struct {
	Balance      string                            `json:"balance"`
	RoundID      string                            `json:"round_id"`
	Transactions []wireRollbackResponseTransaction `json:"transactions"`
}
