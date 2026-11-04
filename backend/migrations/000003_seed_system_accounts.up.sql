-- ---------------------------------------------------------------------------
-- 000003_seed_system_accounts: the shared (SYSTEM-owned) chart of accounts.
--
-- Per-party sub-ledgers (CUSTOMER_RECEIVABLE, MERCHANT_PAYABLE) are created on
-- demand by the ledger repository when an order is authorized.
-- ---------------------------------------------------------------------------
INSERT INTO accounts (code, name, account_type, normal_balance, owner_type, owner_id, currency)
VALUES
    ('CASH_CLEARING',       'Cash & Gateway Clearing', 'ASSET',   'DEBIT',  'SYSTEM', 0, 'USD'),
    ('MERCHANT_FEE_REVENUE','Merchant Fee Revenue',    'REVENUE', 'CREDIT', 'SYSTEM', 0, 'USD'),
    ('LATE_FEE_REVENUE',    'Late Fee Revenue',        'REVENUE', 'CREDIT', 'SYSTEM', 0, 'USD'),
    ('CREDIT_LOSS_EXPENSE', 'Credit Loss Expense',     'EXPENSE', 'DEBIT',  'SYSTEM', 0, 'USD'),
    ('PLATFORM_EQUITY',     'Platform Equity',         'EQUITY',  'CREDIT', 'SYSTEM', 0, 'USD');
