import { useState, type FormEvent } from 'react';

import { useCreateCustomer, useCreateMerchant } from '../api/hooks';
import { ApiError } from '../api/client';
import { dollarsToCents } from '../lib/money';
import type { Customer, Merchant } from '../api/types';

interface Props {
  onCustomerCreated: (customer: Customer) => void;
  onMerchantCreated: (merchant: Merchant) => void;
}

function errorMessage(error: unknown): string {
  if (error instanceof ApiError) {
    return `${error.code}: ${error.message}`;
  }
  return error instanceof Error ? error.message : 'unexpected error';
}

export function OnboardingPanel({ onCustomerCreated, onMerchantCreated }: Props) {
  const createCustomer = useCreateCustomer();
  const createMerchant = useCreateMerchant();

  const [customerRef, setCustomerRef] = useState('');
  const [customerName, setCustomerName] = useState('');
  const [creditLimit, setCreditLimit] = useState('5000.00');

  const [merchantRef, setMerchantRef] = useState('');
  const [merchantName, setMerchantName] = useState('');
  const [feeBps, setFeeBps] = useState('250');

  function handleCreateCustomer(event: FormEvent) {
    event.preventDefault();
    let limit: number;
    try {
      limit = dollarsToCents(creditLimit);
    } catch {
      return;
    }
    createCustomer.mutate(
      {
        externalRef: customerRef.trim(),
        fullName: customerName.trim(),
        creditLimit: limit,
      },
      { onSuccess: onCustomerCreated },
    );
  }

  function handleCreateMerchant(event: FormEvent) {
    event.preventDefault();
    createMerchant.mutate(
      {
        merchantRef: merchantRef.trim(),
        name: merchantName.trim(),
        feeBps: Number.parseInt(feeBps, 10) || 0,
      },
      { onSuccess: onMerchantCreated },
    );
  }

  return (
    <section className="card">
      <h2>Onboarding</h2>

      <form className="form" onSubmit={handleCreateCustomer}>
        <h3>Customer</h3>
        <label>
          External ref
          <input
            value={customerRef}
            onChange={(e) => setCustomerRef(e.target.value)}
            required
          />
        </label>
        <label>
          Full name
          <input
            value={customerName}
            onChange={(e) => setCustomerName(e.target.value)}
            required
          />
        </label>
        <label>
          Credit limit
          <input
            value={creditLimit}
            onChange={(e) => setCreditLimit(e.target.value)}
            inputMode="decimal"
            required
          />
        </label>
        <button type="submit" disabled={createCustomer.isPending}>
          {createCustomer.isPending ? 'Creating…' : 'Create customer'}
        </button>
        {createCustomer.isError && (
          <p className="error">{errorMessage(createCustomer.error)}</p>
        )}
        {createCustomer.data && (
          <p className="ok">
            Customer #{createCustomer.data.id} · limit{' '}
            {createCustomer.data.credit_limit} minor units
          </p>
        )}
      </form>

      <form className="form" onSubmit={handleCreateMerchant}>
        <h3>Merchant</h3>
        <label>
          Merchant ref
          <input
            value={merchantRef}
            onChange={(e) => setMerchantRef(e.target.value)}
            required
          />
        </label>
        <label>
          Name
          <input
            value={merchantName}
            onChange={(e) => setMerchantName(e.target.value)}
            required
          />
        </label>
        <label>
          Fee (basis points)
          <input
            value={feeBps}
            onChange={(e) => setFeeBps(e.target.value)}
            inputMode="numeric"
            required
          />
        </label>
        <button type="submit" disabled={createMerchant.isPending}>
          {createMerchant.isPending ? 'Creating…' : 'Create merchant'}
        </button>
        {createMerchant.isError && (
          <p className="error">{errorMessage(createMerchant.error)}</p>
        )}
        {createMerchant.data && (
          <p className="ok">
            Merchant #{createMerchant.data.id} · fee {createMerchant.data.fee_bps} bps
          </p>
        )}
      </form>
    </section>
  );
}
