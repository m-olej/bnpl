import { useState, type FormEvent } from 'react';

import { useAuthorize } from '../api/hooks';
import { ApiError } from '../api/client';
import { centsToDollars, dollarsToCents } from '../lib/money';

interface Props {
  customerId: string;
  merchantId: string;
  onCustomerIdChange: (value: string) => void;
  onMerchantIdChange: (value: string) => void;
  onAuthorized: (orderRef: string) => void;
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return `${error.code}: ${error.message}`;
  }
  return error instanceof Error ? error.message : 'unexpected error';
}

export function CheckoutForm({
  customerId,
  merchantId,
  onCustomerIdChange,
  onMerchantIdChange,
  onAuthorized,
}: Props) {
  const authorize = useAuthorize();
  const [amount, setAmount] = useState('100.00');
  const [installmentCount, setInstallmentCount] = useState('3');
  const [intervalDays, setIntervalDays] = useState('14');

  function handleSubmit(event: FormEvent) {
    event.preventDefault();
    let principal: number;
    try {
      principal = dollarsToCents(amount);
    } catch {
      return;
    }
    authorize.mutate(
      {
        customerId: Number.parseInt(customerId, 10),
        merchantId: Number.parseInt(merchantId, 10),
        principalAmount: principal,
        installmentCount: Number.parseInt(installmentCount, 10),
        intervalDays: Number.parseInt(intervalDays, 10),
      },
      {
        onSuccess: (data) => onAuthorized(data.order.order_ref),
      },
    );
  }

  return (
    <section className="card">
      <h2>Authorize checkout</h2>
      <form className="form" onSubmit={handleSubmit}>
        <label>
          Customer id
          <input
            value={customerId}
            onChange={(e) => onCustomerIdChange(e.target.value)}
            inputMode="numeric"
            required
          />
        </label>
        <label>
          Merchant id
          <input
            value={merchantId}
            onChange={(e) => onMerchantIdChange(e.target.value)}
            inputMode="numeric"
            required
          />
        </label>
        <label>
          Principal
          <input
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            inputMode="decimal"
            required
          />
        </label>
        <div className="row">
          <label>
            Installments
            <input
              value={installmentCount}
              onChange={(e) => setInstallmentCount(e.target.value)}
              inputMode="numeric"
              required
            />
          </label>
          <label>
            Interval (days)
            <input
              value={intervalDays}
              onChange={(e) => setIntervalDays(e.target.value)}
              inputMode="numeric"
              required
            />
          </label>
        </div>
        <button type="submit" disabled={authorize.isPending}>
          {authorize.isPending ? 'Authorizing…' : 'Authorize order'}
        </button>
        {authorize.isError && (
          <p className="error">{errorMessage(authorize.error)}</p>
        )}
        {authorize.data && (
          <p className="ok">
            Order <code>{authorize.data.order.order_ref}</code> ·{' '}
            {authorize.data.order.status} · fee{' '}
            {centsToDollars(authorize.data.fee_amount)} · merchant payout{' '}
            {centsToDollars(authorize.data.merchant_payout)}
          </p>
        )}
      </form>
    </section>
  );
}
