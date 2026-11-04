-- ---------------------------------------------------------------------------
-- 000001_ledger_core: foundational schema for the BNPL double-entry ledger.
--
-- Money is ALWAYS stored as a signed 64-bit integer of minor units (cents).
-- There are no FLOAT/DOUBLE/DECIMAL monetary columns anywhere. See AGENTS.md
-- section 1 and docs/001.
-- ---------------------------------------------------------------------------

-- ---------------------------------------------------------------------------
-- Parties
-- ---------------------------------------------------------------------------
CREATE TABLE customers (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    external_ref  VARCHAR(64)     NOT NULL,
    full_name     VARCHAR(128)    NOT NULL,
    -- Approved credit limit in minor units. Mutations MUST take a pessimistic
    -- lock (SELECT ... FOR UPDATE) before changing this value.
    credit_limit  BIGINT          NOT NULL DEFAULT 0,
    currency      CHAR(3)         NOT NULL DEFAULT 'USD',
    created_at    DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at    DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
                                  ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_customers_external_ref (external_ref),
    CONSTRAINT chk_customers_credit_limit_non_negative CHECK (credit_limit >= 0)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

CREATE TABLE merchants (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    merchant_ref  VARCHAR(64)     NOT NULL,
    name          VARCHAR(128)    NOT NULL,
    -- Platform take rate in basis points (1 bps = 0.01%). Not money, so INT is
    -- appropriate here.
    fee_bps       INT             NOT NULL DEFAULT 0,
    currency      CHAR(3)         NOT NULL DEFAULT 'USD',
    created_at    DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at    DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
                                  ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_merchants_merchant_ref (merchant_ref),
    CONSTRAINT chk_merchants_fee_bps_range CHECK (fee_bps >= 0 AND fee_bps <= 10000)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- Chart of accounts
--
-- `accounts` carries NO mutable running balance. Balances are derived by
-- summing postings (see internal/db/queries/postings.sql).
-- ---------------------------------------------------------------------------
CREATE TABLE accounts (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    code            VARCHAR(64)     NOT NULL,
    name            VARCHAR(128)    NOT NULL,
    account_type    ENUM('ASSET', 'LIABILITY', 'EQUITY', 'REVENUE', 'EXPENSE')
                                    NOT NULL,
    normal_balance  ENUM('DEBIT', 'CREDIT') NOT NULL,
    -- SYSTEM accounts are shared control accounts (owner_id = 0). CUSTOMER and
    -- MERCHANT accounts are per-party sub-ledgers.
    owner_type      ENUM('SYSTEM', 'CUSTOMER', 'MERCHANT') NOT NULL DEFAULT 'SYSTEM',
    owner_id        BIGINT UNSIGNED NOT NULL DEFAULT 0,
    currency        CHAR(3)         NOT NULL DEFAULT 'USD',
    created_at      DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_accounts_code_owner (code, owner_type, owner_id),
    KEY idx_accounts_owner (owner_type, owner_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- Orders (checkout intent)
-- ---------------------------------------------------------------------------
CREATE TABLE orders (
    id                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    order_ref         VARCHAR(64)     NOT NULL,
    customer_id       BIGINT UNSIGNED NOT NULL,
    merchant_id       BIGINT UNSIGNED NOT NULL,
    principal_amount  BIGINT          NOT NULL,
    currency          CHAR(3)         NOT NULL DEFAULT 'USD',
    installment_count TINYINT UNSIGNED NOT NULL,
    -- Platform fee retained from the merchant payout for this order.
    fee_amount        BIGINT          NOT NULL DEFAULT 0,
    status            ENUM('PENDING', 'AUTHORIZED', 'ACTIVE', 'COMPLETED', 'CANCELLED', 'REFUNDED')
                                      NOT NULL DEFAULT 'PENDING',
    idempotency_key   VARCHAR(255)    NULL,
    created_at        DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at        DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
                                      ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_orders_order_ref (order_ref),
    UNIQUE KEY uq_orders_idempotency_key (idempotency_key),
    KEY idx_orders_customer (customer_id),
    KEY idx_orders_merchant (merchant_id),
    CONSTRAINT fk_orders_customer FOREIGN KEY (customer_id) REFERENCES customers (id),
    CONSTRAINT fk_orders_merchant FOREIGN KEY (merchant_id) REFERENCES merchants (id),
    CONSTRAINT chk_orders_principal_positive CHECK (principal_amount > 0),
    CONSTRAINT chk_orders_fee_non_negative CHECK (fee_amount >= 0),
    CONSTRAINT chk_orders_installment_count CHECK (installment_count >= 1)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- Journal entries (append-only)
-- ---------------------------------------------------------------------------
CREATE TABLE journal_entries (
    id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    entry_type       VARCHAR(48)     NOT NULL,
    reference_type   VARCHAR(48)     NOT NULL,
    reference_id     BIGINT UNSIGNED NOT NULL,
    description      VARCHAR(255)    NOT NULL DEFAULT '',
    trace_id         VARCHAR(64)     NOT NULL DEFAULT '',
    idempotency_key  VARCHAR(255)    NULL,
    occurred_at      DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    created_at       DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_journal_entries_reference (reference_type, reference_id),
    KEY idx_journal_entries_idempotency (idempotency_key)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- Postings (append-only)
--
-- Invariant: for every journal_entry, SUM(DEBIT) - SUM(CREDIT) = 0. Enforced
-- transactionally in internal/ledger and verified by tests.
-- ---------------------------------------------------------------------------
CREATE TABLE postings (
    id                BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    journal_entry_id  BIGINT UNSIGNED NOT NULL,
    account_id        BIGINT UNSIGNED NOT NULL,
    direction         ENUM('DEBIT', 'CREDIT') NOT NULL,
    -- Always a positive magnitude; direction carries the sign.
    amount            BIGINT          NOT NULL,
    currency          CHAR(3)         NOT NULL DEFAULT 'USD',
    created_at        DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_postings_journal_entry (journal_entry_id),
    KEY idx_postings_account (account_id),
    KEY idx_postings_account_direction (account_id, direction),
    CONSTRAINT fk_postings_journal_entry FOREIGN KEY (journal_entry_id)
        REFERENCES journal_entries (id),
    CONSTRAINT fk_postings_account FOREIGN KEY (account_id) REFERENCES accounts (id),
    CONSTRAINT chk_postings_amount_positive CHECK (amount > 0)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- Installment schedule
-- ---------------------------------------------------------------------------
CREATE TABLE installments (
    id                  BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    order_id            BIGINT UNSIGNED NOT NULL,
    customer_id         BIGINT UNSIGNED NOT NULL,
    installment_no      SMALLINT UNSIGNED NOT NULL,
    amount              BIGINT          NOT NULL,
    currency            CHAR(3)         NOT NULL DEFAULT 'USD',
    due_date            DATETIME(6)     NOT NULL,
    status              ENUM('SCHEDULED', 'PROCESSING', 'PAID', 'FAILED', 'CANCELLED')
                                        NOT NULL DEFAULT 'SCHEDULED',
    attempt_count       INT UNSIGNED    NOT NULL DEFAULT 0,
    last_error          VARCHAR(255)    NULL,
    paid_at             DATETIME(6)     NULL,
    journal_entry_id    BIGINT UNSIGNED NULL,
    created_at          DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at          DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
                                        ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    UNIQUE KEY uq_installments_order_no (order_id, installment_no),
    -- Supports the SKIP LOCKED due-installment poll (docs/0003).
    KEY idx_installments_status_due (status, due_date),
    KEY idx_installments_customer (customer_id),
    CONSTRAINT fk_installments_order FOREIGN KEY (order_id) REFERENCES orders (id),
    CONSTRAINT fk_installments_customer FOREIGN KEY (customer_id) REFERENCES customers (id),
    CONSTRAINT fk_installments_journal_entry FOREIGN KEY (journal_entry_id)
        REFERENCES journal_entries (id),
    CONSTRAINT chk_installments_amount_positive CHECK (amount > 0)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

-- ---------------------------------------------------------------------------
-- Idempotency keys (docs/0002)
-- ---------------------------------------------------------------------------
CREATE TABLE idempotency_keys (
    id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    key_value        VARCHAR(255)    NOT NULL,
    endpoint         VARCHAR(255)    NOT NULL,
    request_hash     CHAR(64)        NOT NULL,
    status           ENUM('PROCESSING', 'COMPLETED') NOT NULL DEFAULT 'PROCESSING',
    response_status  INT             NULL,
    response_body    LONGTEXT        NULL,
    created_at       DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at       DATETIME(6)     NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
                                     ON UPDATE CURRENT_TIMESTAMP(6),
    expires_at       DATETIME(6)     NOT NULL,
    PRIMARY KEY (id),
    UNIQUE KEY uq_idempotency_keys_key_endpoint (key_value, endpoint),
    KEY idx_idempotency_keys_expires_at (expires_at)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
