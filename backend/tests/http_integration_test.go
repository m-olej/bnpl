//go:build integration

package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/molej/bnpl/backend/internal/checkout"
	"github.com/molej/bnpl/backend/internal/idempotency"
	"github.com/molej/bnpl/backend/internal/ledger"
	"github.com/molej/bnpl/backend/internal/platform/database"
	"github.com/molej/bnpl/backend/internal/platform/logging"
	"github.com/molej/bnpl/backend/internal/underwriting"
)

type httpHarness struct {
	client       *http.Client
	underwriting *httptest.Server
	checkout     *httptest.Server
}

func newHTTPHarness(t *testing.T) (*httpHarness, func()) {
	t.Helper()
	ctx := context.Background()

	db, err := database.Open(ctx, database.DefaultConfig(testDSN(t)))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}

	logger := logging.New("error")
	repo := ledger.NewRepository(db)
	store := idempotency.NewMySQLStore(db)

	uw := httptest.NewServer(underwriting.NewRouter(underwriting.NewService(repo), store, logger))
	co := httptest.NewServer(checkout.NewRouter(checkout.NewService(repo), store, logger))

	h := &httpHarness{
		client:       &http.Client{Timeout: 10 * time.Second},
		underwriting: uw,
		checkout:     co,
	}
	cleanup := func() {
		uw.Close()
		co.Close()
		_ = db.Close()
	}
	return h, cleanup
}

func (h *httpHarness) do(t *testing.T, method, url, idemKey, body string) (*http.Response, []byte) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = bytes.NewReader([]byte(body))
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("X-Actor-Type", "ADMIN")
	req.Header.Set("X-Actor-Id", "tester")
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp, raw
}

func TestHTTPServicesEndToEnd(t *testing.T) {
	h, cleanup := newHTTPHarness(t)
	defer cleanup()

	suffix := time.Now().UnixNano()
	merchantRef := fmt.Sprintf("m-%d", suffix)
	customerRef := fmt.Sprintf("c-%d", suffix)

	// Onboard a merchant.
	resp, body := h.do(t, http.MethodPost, h.underwriting.URL+"/api/v1/merchants",
		fmt.Sprintf("merch-%d", suffix), fmt.Sprintf(`{"merchant_ref":%q,"name":"Acme","fee_bps":250}`, merchantRef))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create merchant status = %d body=%s", resp.StatusCode, body)
	}
	var merchant struct {
		ID int64 `json:"id"`
	}
	mustJSON(t, body, &merchant)

	// Onboard a customer with a generous limit.
	resp, body = h.do(t, http.MethodPost, h.underwriting.URL+"/api/v1/customers",
		fmt.Sprintf("cust-%d", suffix), fmt.Sprintf(`{"external_ref":%q,"full_name":"Jane Doe","credit_limit":100000}`, customerRef))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create customer status = %d body=%s", resp.StatusCode, body)
	}
	var customer struct {
		ID int64 `json:"id"`
	}
	mustJSON(t, body, &customer)

	// Credit position starts fully available.
	resp, body = h.do(t, http.MethodGet, h.underwriting.URL+"/api/v1/customers/"+customerRef+"/credit", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get credit status = %d body=%s", resp.StatusCode, body)
	}
	var credit struct {
		Available int64 `json:"available"`
	}
	mustJSON(t, body, &credit)
	if credit.Available != 100000 {
		t.Fatalf("available = %d, want 100000", credit.Available)
	}

	// Authorize an order.
	authBody := fmt.Sprintf(
		`{"customer_id":%d,"merchant_id":%d,"principal_amount":10000,"installment_count":3,"interval_days":14}`,
		customer.ID, merchant.ID)
	authKey := fmt.Sprintf("auth-%d", suffix)
	resp, body = h.do(t, http.MethodPost, h.checkout.URL+"/api/v1/checkout/authorize", authKey, authBody)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("authorize status = %d body=%s", resp.StatusCode, body)
	}
	var authorized struct {
		Order struct {
			OrderRef string `json:"order_ref"`
		} `json:"order"`
		Installments []struct {
			Amount int64 `json:"amount"`
		} `json:"installments"`
	}
	mustJSON(t, body, &authorized)
	if authorized.Order.OrderRef == "" {
		t.Fatal("expected server-generated order_ref")
	}
	wantAmounts := []int64{3334, 3333, 3333}
	if len(authorized.Installments) != 3 {
		t.Fatalf("got %d installments, want 3", len(authorized.Installments))
	}
	for i, inst := range authorized.Installments {
		if inst.Amount != wantAmounts[i] {
			t.Errorf("installment[%d] amount = %d, want %d", i, inst.Amount, wantAmounts[i])
		}
	}

	// Replaying the same request returns the cached response.
	resp2, body2 := h.do(t, http.MethodPost, h.checkout.URL+"/api/v1/checkout/authorize", authKey, authBody)
	if resp2.StatusCode != http.StatusCreated {
		t.Fatalf("replay status = %d body=%s", resp2.StatusCode, body2)
	}
	if resp2.Header.Get("Idempotent-Replay") != "true" {
		t.Error("expected Idempotent-Replay header on replayed response")
	}
	var replayed struct {
		Order struct {
			OrderRef string `json:"order_ref"`
		} `json:"order"`
	}
	mustJSON(t, body2, &replayed)
	if replayed.Order.OrderRef != authorized.Order.OrderRef {
		t.Errorf("replay order_ref = %s, want %s", replayed.Order.OrderRef, authorized.Order.OrderRef)
	}

	// Same key, different payload -> 422.
	resp, body = h.do(t, http.MethodPost, h.checkout.URL+"/api/v1/checkout/authorize", authKey,
		fmt.Sprintf(`{"customer_id":%d,"merchant_id":%d,"principal_amount":9999,"installment_count":3}`, customer.ID, merchant.ID))
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("payload mismatch status = %d, want 422 body=%s", resp.StatusCode, body)
	}

	// Missing idempotency key -> 400.
	resp, body = h.do(t, http.MethodPost, h.checkout.URL+"/api/v1/checkout/authorize", "", authBody)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing key status = %d, want 400 body=%s", resp.StatusCode, body)
	}

	// Fetch the order.
	resp, body = h.do(t, http.MethodGet, h.checkout.URL+"/api/v1/orders/"+authorized.Order.OrderRef, "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("get order status = %d body=%s", resp.StatusCode, body)
	}

	// Credit now reflects the outstanding principal.
	resp, body = h.do(t, http.MethodGet, h.underwriting.URL+"/api/v1/customers/"+customerRef+"/credit", "", "")
	mustJSON(t, body, &credit)
	if credit.Available != 90000 {
		t.Fatalf("available after order = %d, want 90000", credit.Available)
	}

	// Underwriting decision: large amount is declined.
	resp, body = h.do(t, http.MethodGet,
		h.underwriting.URL+"/api/v1/underwriting/eligibility?customer_ref="+customerRef+"&amount=500000", "", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("eligibility status = %d body=%s", resp.StatusCode, body)
	}
	var decision struct {
		Approved bool `json:"approved"`
	}
	mustJSON(t, body, &decision)
	if decision.Approved {
		t.Error("expected large amount to be declined")
	}
}

func TestHTTPAuthorizeInsufficientCredit(t *testing.T) {
	h, cleanup := newHTTPHarness(t)
	defer cleanup()

	suffix := time.Now().UnixNano()
	merchantRef := fmt.Sprintf("m2-%d", suffix)

	resp, body := h.do(t, http.MethodPost, h.underwriting.URL+"/api/v1/merchants",
		fmt.Sprintf("merch2-%d", suffix), fmt.Sprintf(`{"merchant_ref":%q,"name":"Tiny","fee_bps":0}`, merchantRef))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create merchant status = %d body=%s", resp.StatusCode, body)
	}
	var merchant struct {
		ID int64 `json:"id"`
	}
	mustJSON(t, body, &merchant)

	customerRef := fmt.Sprintf("c2-%d", suffix)
	resp, body = h.do(t, http.MethodPost, h.underwriting.URL+"/api/v1/customers",
		fmt.Sprintf("cust2-%d", suffix), fmt.Sprintf(`{"external_ref":%q,"full_name":"Low Limit","credit_limit":5000}`, customerRef))
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create customer status = %d body=%s", resp.StatusCode, body)
	}
	var customer struct {
		ID int64 `json:"id"`
	}
	mustJSON(t, body, &customer)

	authBody := fmt.Sprintf(
		`{"customer_id":%d,"merchant_id":%d,"principal_amount":10000,"installment_count":4}`, customer.ID, merchant.ID)
	resp, body = h.do(t, http.MethodPost, h.checkout.URL+"/api/v1/checkout/authorize", fmt.Sprintf("auth2-%d", suffix), authBody)
	if resp.StatusCode != http.StatusPaymentRequired {
		t.Fatalf("expected 402 for insufficient credit, got %d body=%s", resp.StatusCode, body)
	}
}

func mustJSON(t *testing.T, raw []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("unmarshal %s: %v", string(raw), err)
	}
}
