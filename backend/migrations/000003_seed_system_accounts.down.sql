DELETE FROM accounts
WHERE owner_type = 'SYSTEM'
  AND code IN (
      'CASH_CLEARING',
      'MERCHANT_FEE_REVENUE',
      'LATE_FEE_REVENUE',
      'CREDIT_LOSS_EXPENSE',
      'PLATFORM_EQUITY'
  );
