-- Reverse of 000001_ledger_core. Drop children before parents.
DROP TABLE IF EXISTS idempotency_keys;
DROP TABLE IF EXISTS installments;
DROP TABLE IF EXISTS postings;
DROP TABLE IF EXISTS journal_entries;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS accounts;
DROP TABLE IF EXISTS merchants;
DROP TABLE IF EXISTS customers;
