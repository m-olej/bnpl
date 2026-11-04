import { expect, test } from '@playwright/test';

// One end-to-end checkout: onboard a customer and merchant, authorize an order,
// inspect the installment schedule, issue a partial refund, then refund the
// remainder and confirm the order becomes REFUNDED.
test('onboard, authorize, inspect schedule and refund', async ({ page }) => {
  const suffix = Date.now().toString();

  await page.goto('/');

  await page.getByLabel('External ref').fill(`e2e-cust-${suffix}`);
  await page.getByLabel('Full name', { exact: true }).fill('E2E Customer');
  await page.getByLabel('Credit limit').fill('5000.00');
  await page.getByRole('button', { name: 'Create customer' }).click();
  await expect(page.getByTestId('customer-created')).toContainText('Customer #');

  await page.getByLabel('Merchant ref').fill(`e2e-merch-${suffix}`);
  await page.getByLabel('Name', { exact: true }).fill('E2E Merchant');
  await page.getByLabel('Fee (basis points)').fill('250');
  await page.getByRole('button', { name: 'Create merchant' }).click();
  await expect(page.getByTestId('merchant-created')).toContainText('Merchant #');

  // Onboarding auto-fills the customer and merchant ids.
  await page.getByLabel('Principal').fill('100.00');
  await page.getByLabel('Installments').fill('3');
  await page.getByRole('button', { name: 'Authorize order' }).click();
  await expect(page.getByTestId('authorize-result')).toContainText('AUTHORIZED');

  await expect(page.getByTestId('order-status')).toHaveText('AUTHORIZED');
  await expect(page.getByTestId('installment-row')).toHaveCount(3);

  // Partial refund of a third of the principal.
  await page.getByLabel('Amount').fill('25.00');
  await page.getByLabel('Reason').fill('e2e partial refund');
  await page.getByRole('button', { name: 'Issue refund' }).click();
  await expect(page.getByTestId('refund-result')).toContainText('remaining 75.00');
  await expect(page.getByTestId('order-status')).toHaveText('AUTHORIZED');

  // Refund the remainder; the order is fully refunded.
  await page.getByLabel('Amount').fill('75.00');
  await page.getByRole('button', { name: 'Issue refund' }).click();
  await expect(page.getByTestId('order-status')).toHaveText('REFUNDED');
  await expect(page.getByTestId('refund-result')).toContainText('remaining 0.00');
});
