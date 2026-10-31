package web

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/candylario/candylario/trader/api"
)

// The wallet page connects an account's own Hyperliquid wallet without a
// key being pasted: the engine makes an agent wallet, the browser wallet
// signs its approval (and the builder fee's, when there is one), and the
// engine confirms both with the exchange. The page's script does the
// signing; these handlers pass the approvals between it and the engine.

// walletMode is the exchange accounts connect on: testnet, until the
// testnet run is proven.
const walletMode = "testnet"

type walletPage struct {
	Wallet  *api.Wallet // nil until the engine has made one
	Mode    string
	Problem string
}

func (s *Server) wallet(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := callCtx(r)
	defer cancel()
	p := walletPage{Mode: walletMode}
	wl, err := s.Engine.Wallet(ctx, current(r).User.TenantID.String())
	switch {
	case isCode(err, "not_found"):
	case err != nil:
		s.Logf("engine: %v", err)
		p.Problem = "The engine is not reachable right now; try again in a minute."
	default:
		p.Wallet = &wl
	}
	s.render(w, r, "wallet.html", http.StatusOK, page{Title: "Wallet", Data: p})
}

// walletApprovals gives the page's script what the browser wallet signs.
func (s *Server) walletApprovals(w http.ResponseWriter, r *http.Request) {
	var in struct {
		User    string `json:"user"`
		ChainID int64  `json:"chain_id"`
	}
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "That request was not understood; reload the page."})
		return
	}
	ctx, cancel := callCtx(r)
	defer cancel()
	a, err := s.Engine.WalletApprovals(ctx, current(r).User.TenantID.String(), in.User, walletMode, in.ChainID)
	if err != nil {
		s.walletFailed(w, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

// walletApprove passes the signed approvals to the engine.
func (s *Server) walletApprove(w http.ResponseWriter, r *http.Request) {
	var in api.WalletApproval
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&in) != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "That request was not understood; reload the page."})
		return
	}
	in.Mode = walletMode
	ctx, cancel := callCtx(r)
	defer cancel()
	wl, err := s.Engine.ApproveWallet(ctx, current(r).User.TenantID.String(), in)
	if err != nil {
		s.walletFailed(w, err)
		return
	}
	s.flash(w, "Wallet connected. The buttons under each alert in your Telegram chat now trade from it.")
	writeJSON(w, http.StatusOK, wl)
}

// walletFailed tells the page's script why, in words for the user.
func (s *Server) walletFailed(w http.ResponseWriter, err error) {
	status := http.StatusServiceUnavailable
	var e *api.Error
	if errors.As(err, &e) && e.Status < 500 {
		status = http.StatusUnprocessableEntity
	} else {
		s.Logf("engine: %v", err)
	}
	writeJSON(w, status, map[string]string{"error": userMessage(err)})
}
