// A complete Beexar wallet integration in one file.
//
//	BEEXAR_API_SECRET=... go run ./examples/server
//
// Point the four callback URLs in the backoffice at
// http://<host>/wallet/{balance,betwin,rollback,finish} and run the Integration
// Test Game against it.
package main

import (
	"log"
	"net/http"
	"os"

	beexar "github.com/beexar-games/beexar-go"
	"github.com/beexar-games/beexar-go/examples/inmemory"
)

func main() {
	apiSecret := os.Getenv("BEEXAR_API_SECRET")
	if apiSecret == "" {
		log.Fatal("BEEXAR_API_SECRET is not set — refusing to start without a secret")
	}

	wallet := inmemory.New(inmemory.LedgerState{
		Accounts: map[string]inmemory.StoredAccount{
			"player_1": {Currency: "EUR", Balance: "1000.00"},
		},
	})

	server, err := beexar.NewWalletServer(wallet, apiSecret,
		beexar.WithWarning(func(m string) { log.Printf("beexar: %s", m) }),
	)
	if err != nil {
		log.Fatal(err)
	}

	addr := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}

	mux := http.NewServeMux()
	mux.Handle("/wallet/", server.NewHandler())

	log.Printf("beexar wallet listening on %s", addr)
	log.Printf("  POST /wallet/balance  /wallet/betwin  /wallet/rollback  /wallet/finish")
	// ReadHeaderTimeout keeps a slow-header client from holding a connection open.
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10_000_000_000}
	log.Fatal(srv.ListenAndServe())
}
