import { useState } from 'react';

import { CheckoutForm } from './components/CheckoutForm';
import { OnboardingPanel } from './components/OnboardingPanel';
import { OrderPanel } from './components/OrderPanel';

export default function App() {
  const [customerId, setCustomerId] = useState('');
  const [merchantId, setMerchantId] = useState('');
  const [customerRef, setCustomerRef] = useState('');
  const [activeOrderRef, setActiveOrderRef] = useState<string | null>(null);

  return (
    <main className="page">
      <header className="page__header">
        <h1>BNPL Checkout Console</h1>
        <p>
          Onboard a party, authorize an order, inspect its installment
          schedule, and issue partial refunds against the outstanding principal.
        </p>
      </header>

      <div className="grid">
        <OnboardingPanel
          onCustomerCreated={(customer) => {
            setCustomerId(String(customer.id));
            setCustomerRef(customer.external_ref);
          }}
          onMerchantCreated={(merchant) => setMerchantId(String(merchant.id))}
        />
        <CheckoutForm
          customerId={customerId}
          merchantId={merchantId}
          onCustomerIdChange={setCustomerId}
          onMerchantIdChange={setMerchantId}
          onAuthorized={setActiveOrderRef}
        />
      </div>

      {activeOrderRef !== null && (
        <OrderPanel orderRef={activeOrderRef} customerRef={customerRef} />
      )}
    </main>
  );
}
