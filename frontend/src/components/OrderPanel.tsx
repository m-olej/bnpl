import { useState, type FormEvent } from 'react';

import { useCreditPosition, useOrder, useRefund } from '../api/hooks';
import { ApiError } from '../api/client';
import { centsToDollars, dollarsToCents } from '../lib/money';

interface Props {
  orderRef: string;
  customerRef: string;
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return `${error.code}: ${error.message}`;
  }
  return error instanceof Error ? error.message : 'unexpected error';
}

export function OrderPanel({ orderRef, customerRef }: Props) {
  const order = useOrder(orderRef);
  const credit = useCreditPosition(customerRef);
  const refund = useRefund();
  const [amount, setAmount] = useState('');
  const [reason, setReason] = useState('');

  function handleRefund(event: FormEvent) {
    event.preventDefault();
    let cents: number;
    try {
      cents = dollarsToCents(amount);
    } catch {
      return;
    }
    refund.mutate(
      { orderRef, amount: cents, reason: reason.trim() },
      { onSuccess: () => setAmount('') },
    );
  }

  if (order.isLoading) {
    return (
      <section className="card">
        <p>Loading order…</p>
      </section>
    );
  }

  if (order.isError || !order.data) {
    return (
      <section className="card">
        <p className="error">{errorMessage(order.error)}</p>
      </section>
    );
  }

  const { order: detail, installments } = order.data;

  return (
    <section className="card">
      <h2>
        Order <code>{detail.order_ref}</code>
      </h2>
      <dl className="facts">
        <div>
          <dt>Status</dt>
          <dd>{detail.status}</dd>
        </div>
        <div>
          <dt>Principal</dt>
          <dd>{centsToDollars(detail.principal_amount)}</dd>
        </div>
        <div>
          <dt>Fee</dt>
          <dd>{centsToDollars(detail.fee_amount)}</dd>
        </div>
        <div>
          <dt>Currency</dt>
          <dd>{detail.currency}</dd>
        </div>
      </dl>

      {credit.data && (
        <p className="muted">
          Available credit {centsToDollars(credit.data.available)} · outstanding{' '}
          {centsToDollars(credit.data.outstanding)}
        </p>
      )}

      <h3>Installment schedule</h3>
      <table className="table">
        <thead>
          <tr>
            <th>#</th>
            <th>Amount</th>
            <th>Due</th>
            <th>Status</th>
          </tr>
        </thead>
        <tbody>
          {installments.map((inst) => (
            <tr key={inst.id}>
              <td>{inst.number}</td>
              <td>{centsToDollars(inst.amount)}</td>
              <td>{new Date(inst.due_date).toLocaleDateString()}</td>
              <td>{inst.status}</td>
            </tr>
          ))}
        </tbody>
      </table>

      <form className="form" onSubmit={handleRefund}>
        <h3>Refund</h3>
        <p className="muted">
          Refunds reduce the outstanding principal only. The fee is reversed
          proportionally and a full refund marks the order REFUNDED.
        </p>
        <label>
          Amount
          <input
            value={amount}
            onChange={(e) => setAmount(e.target.value)}
            inputMode="decimal"
            placeholder="e.g. 25.00"
            required
          />
        </label>
        <label>
          Reason
          <input
            value={reason}
            onChange={(e) => setReason(e.target.value)}
            placeholder="optional"
          />
        </label>
        <button
          type="submit"
          disabled={refund.isPending || detail.status === 'REFUNDED'}
        >
          {refund.isPending ? 'Refunding…' : 'Issue refund'}
        </button>
        {refund.isError && <p className="error">{errorMessage(refund.error)}</p>}
        {refund.data && (
          <p className="ok">
            Refunded {centsToDollars(refund.data.refunded_amount)} · fee reversed{' '}
            {centsToDollars(refund.data.fee_refunded)} · merchant reduction{' '}
            {centsToDollars(refund.data.merchant_reduction)} · remaining{' '}
            {centsToDollars(refund.data.remaining_outstanding)} · status{' '}
            {refund.data.order_status}
          </p>
        )}
      </form>
    </section>
  );
}
