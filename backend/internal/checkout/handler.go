package checkout

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/molej/bnpl/backend/api"
	"github.com/molej/bnpl/backend/internal/idempotency"
	"github.com/molej/bnpl/backend/internal/ledger"
	"github.com/molej/bnpl/backend/internal/platform/httpkit"
)

// Handler exposes the checkout HTTP API.
type Handler struct {
	svc *Service
}

// NewRouter builds the fully-wrapped checkout handler.
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
	mux.HandleFunc("POST /api/v1/checkout/authorize", h.authorize)
	mux.HandleFunc("GET /api/v1/orders/{orderRef}", h.getOrder)
	mux.HandleFunc("GET /api/v1/orders/{orderRef}/schedule", h.getSchedule)
	mux.HandleFunc("POST /api/v1/orders/{orderRef}/refunds", h.refund)
	mux.HandleFunc("GET /openapi.yaml", api.SpecHandler)
	mux.HandleFunc("GET /docs", api.DocsHandler)
	return mux
}

type authorizeRequest struct {
	CustomerID       int64      `json:"customer_id"`
	MerchantID       int64      `json:"merchant_id"`
	PrincipalAmount  int64      `json:"principal_amount"`
	Currency         string     `json:"currency"`
	InstallmentCount int        `json:"installment_count"`
	IntervalDays     int        `json:"interval_days"`
	FirstDueDate     *time.Time `json:"first_due_date"`
}

type orderResponse struct {
	ID               int64     `json:"id"`
	OrderRef         string    `json:"order_ref"`
	CustomerID       int64     `json:"customer_id"`
	MerchantID       int64     `json:"merchant_id"`
	PrincipalAmount  int64     `json:"principal_amount"`
	Currency         string    `json:"currency"`
	InstallmentCount int       `json:"installment_count"`
	FeeAmount        int64     `json:"fee_amount"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
}

type installmentResponse struct {
	ID       int64      `json:"id"`
	Number   int        `json:"number"`
	Amount   int64      `json:"amount"`
	Currency string     `json:"currency"`
	DueDate  time.Time  `json:"due_date"`
	Status   string     `json:"status"`
	PaidAt   *time.Time `json:"paid_at,omitempty"`
}

type authorizeResponse struct {
	Order          orderResponse         `json:"order"`
	Installments   []installmentResponse `json:"installments"`
	FeeAmount      int64                 `json:"fee_amount"`
	MerchantPayout int64                 `json:"merchant_payout"`
}

type orderDetailResponse struct {
	Order        orderResponse         `json:"order"`
	Installments []installmentResponse `json:"installments"`
}

type refundRequest struct {
	Amount int64  `json:"amount"`
	Reason string `json:"reason"`
}

type refundResponse struct {
	OrderID              int64  `json:"order_id"`
	OrderRef             string `json:"order_ref"`
	JournalEntryID       int64  `json:"journal_entry_id"`
	RefundedAmount       int64  `json:"refunded_amount"`
	FeeRefunded          int64  `json:"fee_refunded"`
	MerchantReduction    int64  `json:"merchant_reduction"`
	OrderStatus          string `json:"order_status"`
	RemainingOutstanding int64  `json:"remaining_outstanding"`
}

func (h *Handler) authorize(w http.ResponseWriter, r *http.Request) {
	var req authorizeRequest
	if !httpkit.DecodeJSON(w, r, &req) {
		return
	}
	if req.CustomerID <= 0 || req.MerchantID <= 0 || req.PrincipalAmount <= 0 {
		httpkit.WriteError(w, http.StatusBadRequest, "invalid_request",
			"customer_id, merchant_id and principal_amount must be positive")
		return
	}

	key := r.Header.Get("Idempotency-Key")
	out, err := h.svc.Authorize(r.Context(), AuthorizeInput{
		CustomerID:       req.CustomerID,
		MerchantID:       req.MerchantID,
		PrincipalAmount:  req.PrincipalAmount,
		Currency:         req.Currency,
		InstallmentCount: req.InstallmentCount,
		IntervalDays:     req.IntervalDays,
		FirstDueDate:     req.FirstDueDate,
		IdempotencyKey:   &key,
		TraceID:          httpkit.RequestIDFromContext(r.Context()),
	})
	if err != nil {
		httpkit.WriteMappedError(w, err)
		return
	}

	httpkit.WriteJSON(w, http.StatusCreated, authorizeResponse{
		Order:          toOrderResponse(out.Order),
		Installments:   toInstallmentResponses(out.Installments),
		FeeAmount:      out.FeeAmount,
		MerchantPayout: out.MerchantPayout,
	})
}

func (h *Handler) getOrder(w http.ResponseWriter, r *http.Request) {
	order, installments, err := h.svc.GetOrder(r.Context(), r.PathValue("orderRef"))
	if err != nil {
		httpkit.WriteMappedError(w, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, orderDetailResponse{
		Order:        toOrderResponse(order),
		Installments: toInstallmentResponses(installments),
	})
}

func (h *Handler) getSchedule(w http.ResponseWriter, r *http.Request) {
	installments, err := h.svc.Schedule(r.Context(), r.PathValue("orderRef"))
	if err != nil {
		httpkit.WriteMappedError(w, err)
		return
	}
	httpkit.WriteJSON(w, http.StatusOK, map[string]any{"installments": toInstallmentResponses(installments)})
}

func (h *Handler) refund(w http.ResponseWriter, r *http.Request) {
	var req refundRequest
	if !httpkit.DecodeJSON(w, r, &req) {
		return
	}
	if req.Amount <= 0 {
		httpkit.WriteError(w, http.StatusBadRequest, "invalid_request", "amount must be positive")
		return
	}

	key := r.Header.Get("Idempotency-Key")
	orderRef := r.PathValue("orderRef")
	result, err := h.svc.Refund(r.Context(), RefundInput{
		OrderRef:       orderRef,
		Amount:         req.Amount,
		Reason:         req.Reason,
		IdempotencyKey: &key,
		TraceID:        httpkit.RequestIDFromContext(r.Context()),
	})
	if err != nil {
		httpkit.WriteMappedError(w, err)
		return
	}

	httpkit.WriteJSON(w, http.StatusCreated, refundResponse{
		OrderID:              result.OrderID,
		OrderRef:             orderRef,
		JournalEntryID:       result.JournalEntryID,
		RefundedAmount:       result.RefundedAmount,
		FeeRefunded:          result.FeeRefunded,
		MerchantReduction:    result.MerchantReduction,
		OrderStatus:          result.OrderStatus,
		RemainingOutstanding: result.RemainingOutstanding,
	})
}

func toOrderResponse(o ledger.Order) orderResponse {
	return orderResponse{
		ID:               o.ID,
		OrderRef:         o.OrderRef,
		CustomerID:       o.CustomerID,
		MerchantID:       o.MerchantID,
		PrincipalAmount:  o.PrincipalAmount,
		Currency:         o.Currency,
		InstallmentCount: o.InstallmentCount,
		FeeAmount:        o.FeeAmount,
		Status:           o.Status,
		CreatedAt:        o.CreatedAt,
	}
}

func toInstallmentResponses(in []ledger.Installment) []installmentResponse {
	out := make([]installmentResponse, 0, len(in))
	for _, inst := range in {
		out = append(out, installmentResponse{
			ID:       inst.ID,
			Number:   inst.Number,
			Amount:   inst.Amount,
			Currency: inst.Currency,
			DueDate:  inst.DueDate,
			Status:   inst.Status,
			PaidAt:   inst.PaidAt,
		})
	}
	return out
}
