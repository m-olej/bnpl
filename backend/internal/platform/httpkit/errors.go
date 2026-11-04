package httpkit

import (
	"errors"
	"net/http"

	"github.com/molej/bnpl/backend/internal/ledger"
)

// MappedError is a domain error translated into an HTTP response.
type MappedError struct {
	Status  int
	Code    string
	Message string
}

// MapError translates a domain/transport error into an HTTP status and a
// machine-readable code. Unknown errors become 500 with a generic message so
// internal details are not leaked.
func MapError(err error) MappedError {
	switch {
	case err == nil:
		return MappedError{Status: http.StatusOK, Code: "ok", Message: ""}

	case errors.Is(err, ledger.ErrInsufficientCredit):
		return MappedError{Status: http.StatusPaymentRequired, Code: "insufficient_credit", Message: err.Error()}

	case errors.Is(err, ledger.ErrOrderNotFound),
		errors.Is(err, ledger.ErrCustomerNotFound),
		errors.Is(err, ledger.ErrMerchantNotFound),
		errors.Is(err, ledger.ErrInstallmentNotFound),
		errors.Is(err, ledger.ErrAccountNotFound):
		return MappedError{Status: http.StatusNotFound, Code: "not_found", Message: err.Error()}

	case errors.Is(err, ledger.ErrInstallmentNotPayable):
		return MappedError{Status: http.StatusConflict, Code: "installment_not_payable", Message: err.Error()}

	case errors.Is(err, ledger.ErrInvalidOrder),
		errors.Is(err, ledger.ErrInvalidFee),
		errors.Is(err, ledger.ErrInvalidInstallmentSplit):
		return MappedError{Status: http.StatusBadRequest, Code: "invalid_request", Message: err.Error()}

	case errors.Is(err, ledger.ErrUnbalancedJournal),
		errors.Is(err, ledger.ErrTooFewPostings),
		errors.Is(err, ledger.ErrNonPositiveAmount),
		errors.Is(err, ledger.ErrUnknownDirection),
		errors.Is(err, ledger.ErrMixedCurrency),
		errors.Is(err, ledger.ErrAmountOverflow):
		return MappedError{Status: http.StatusUnprocessableEntity, Code: "invalid_journal", Message: err.Error()}

	default:
		return MappedError{Status: http.StatusInternalServerError, Code: "internal_error", Message: "internal server error"}
	}
}

// WriteMappedError writes the mapped error to the response.
func WriteMappedError(w http.ResponseWriter, err error) {
	m := MapError(err)
	WriteError(w, m.Status, m.Code, m.Message)
}
