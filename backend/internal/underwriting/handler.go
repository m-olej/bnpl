package underwriting

import (
	"log/slog"
	"net/http"
	"strconv"

	"github.com/molej/bnpl/backend/api"
	"github.com/molej/bnpl/backend/internal/idempotency"
	"github.com/molej/bnpl/backend/internal/ledger"
	"github.com/molej/bnpl/backend/internal/platform/httpkit"
)

// Handler exposes the underwriting HTTP API.
type Handler struct {
	svc *Service
}

// NewRouter builds the fully-wrapped underwriting handler.
func NewRouter(svc *Service, store idempotency.Claimer, logger *slog.Logger) http.Handler {
	h := &Handler{svc: svc}
	return httpkit.Chain(h.routes(),
		httpkit.RequestID(),
		httpkit.Logging(logger),
		httpkit.Auth(),
		idempotency.Middleware(store, idempotency.WithLogger(logger)),
		httpkit.Recoverer(logger),
	)
}

func (h *Handler) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/customers", h.createCustomer)
	mux.HandleFunc("GET /api/v1/customers/{externalRef}", h.getCustomer)
	mux.HandleFunc("PATCH /api/v1/customers/{externalRef}/limit", h.setLimit)
	mux.HandleFunc("GET /api/v1/customers/{externalRef}/credit", h.getCredit)
	mux.HandleFunc("POST /api/v1/merchants", h.createMerchant)
	mux.HandleFunc("GET /api/v1/merchants/{merchantRef}", h.getMerchant)
	mux.HandleFunc("GET /api/v1/underwriting/eligibility", h.eligibility)
	mux.HandleFunc("GET /openapi.yaml", api.SpecHandler)
	mux.HandleFunc("GET /docs", api.DocsHandler)
	return mux
}

type createCustomerRequest struct {
	ExternalRef string `json:"external_ref"`
	FullName    string `json:"full_name"`
	CreditLimit int64  `json:"credit_limit"`
	Currency    string `json:"currency"`
}

type customerResponse struct {
	ID          int64  `json:"id"`
	ExternalRef string `json:"external_ref"`
	FullName    string `json:"full_name"`
	CreditLimit int64  `json:"credit_limit"`
	Currency    string `json:"currency"`
}

type setLimitRequest struct {
	CreditLimit int64 `json:"credit_limit"`
}

type creditResponse struct {
	CustomerID  int64  `json:"customer_id"`
	ExternalRef string `json:"external_ref"`
	CreditLimit int64  `json:"credit_limit"`
	Outstanding int64  `json:"outstanding"`
	Available   int64  `json:"available"`
	Currency    string `json:"currency"`
}

type createMerchantRequest struct {
	MerchantRef string `json:"merchant_ref"`
	Name        string `json:"name"`
	FeeBps      int    `json:"fee_bps"`
	Currency    string `json:"currency"`
}

type merchantResponse struct {
	ID          int64  `json:"id"`
	MerchantRef string `json:"merchant_ref"`
	Name        string `json:"name"`
	FeeBps      int    `json:"fee_bps"`
	Currency    string `json:"currency"`
}

type decisionResponse struct {
	Approved  bool   `json:"approved"`
	Requested int64  `json:"requested"`
	Available int64  `json:"available"`
	Reason    string `json:"reason"`
}

func (h *Handler) createCustomer(w http.ResponseWriter, r *http.Request) {
	var req createCustomerRequest
	if !httpkit.DecodeJSON(w, r, &req) {
		return
	}
	if req.ExternalRef == "" || req.FullName == "" {
		httpkit.WriteError(w, http.StatusBadRequest, "invalid_request", "external_ref and full_name are required")
		return
	}
	cust, err := h.svc.CreateCustomer(r.Context(), CreateCustomerInput(req))
	if err != nil {
		httpkit.WriteMappedError(w, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusCreated, toCustomerResponse(cust))
}

func (h *Handler) getCustomer(w http.ResponseWriter, r *http.Request) {
	cust, err := h.svc.GetCustomerByRef(r.Context(), r.PathValue("externalRef"))
	if err != nil {
		httpkit.WriteMappedError(w, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, toCustomerResponse(cust))
}

func (h *Handler) setLimit(w http.ResponseWriter, r *http.Request) {
	var req setLimitRequest
	if !httpkit.DecodeJSON(w, r, &req) {
		return
	}
	cust, err := h.svc.SetLimitByRef(r.Context(), r.PathValue("externalRef"), req.CreditLimit)
	if err != nil {
		httpkit.WriteMappedError(w, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, toCustomerResponse(cust))
}

func (h *Handler) getCredit(w http.ResponseWriter, r *http.Request) {
	view, err := h.svc.Credit(r.Context(), r.PathValue("externalRef"))
	if err != nil {
		httpkit.WriteMappedError(w, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, creditResponse{
		CustomerID:  view.Customer.ID,
		ExternalRef: view.Customer.ExternalRef,
		CreditLimit: view.Customer.CreditLimit,
		Outstanding: view.Outstanding,
		Available:   view.Available,
		Currency:    view.Customer.Currency,
	})
}

func (h *Handler) createMerchant(w http.ResponseWriter, r *http.Request) {
	var req createMerchantRequest
	if !httpkit.DecodeJSON(w, r, &req) {
		return
	}
	if req.MerchantRef == "" || req.Name == "" {
		httpkit.WriteError(w, http.StatusBadRequest, "invalid_request", "merchant_ref and name are required")
		return
	}
	merchant, err := h.svc.CreateMerchant(r.Context(), CreateMerchantInput(req))
	if err != nil {
		httpkit.WriteMappedError(w, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusCreated, toMerchantResponse(merchant))
}

func (h *Handler) getMerchant(w http.ResponseWriter, r *http.Request) {
	merchant, err := h.svc.GetMerchantByRef(r.Context(), r.PathValue("merchantRef"))
	if err != nil {
		httpkit.WriteMappedError(w, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, toMerchantResponse(merchant))
}

func (h *Handler) eligibility(w http.ResponseWriter, r *http.Request) {
	ref := r.URL.Query().Get("customer_ref")
	amount, err := strconv.ParseInt(r.URL.Query().Get("amount"), 10, 64)
	if err != nil {
		httpkit.WriteError(w, http.StatusBadRequest, "invalid_request", "amount must be an integer in minor units")
		return
	}
	decision, err := h.svc.Evaluate(r.Context(), ref, amount)
	if err != nil {
		httpkit.WriteMappedError(w, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, decisionResponse(decision))
}

func toCustomerResponse(c ledger.Customer) customerResponse {
	return customerResponse{
		ID:          c.ID,
		ExternalRef: c.ExternalRef,
		FullName:    c.FullName,
		CreditLimit: c.CreditLimit,
		Currency:    c.Currency,
	}
}

func toMerchantResponse(m ledger.Merchant) merchantResponse {
	return merchantResponse{
		ID:          m.ID,
		MerchantRef: m.MerchantRef,
		Name:        m.Name,
		FeeBps:      m.FeeBps,
		Currency:    m.Currency,
	}
}
