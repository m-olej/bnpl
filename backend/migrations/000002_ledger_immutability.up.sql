-- ---------------------------------------------------------------------------
-- 000002_ledger_immutability: enforce append-only semantics at the database
-- boundary. Journal entries and postings must never be UPDATEd or DELETEd;
-- corrections are represented by compensating entries (docs/001).
--
-- NOTE: this file is intentionally excluded from sqlc's schema model (see
-- backend/sqlc.yaml) because triggers are not part of the queryable model.
-- ---------------------------------------------------------------------------

CREATE TRIGGER trg_journal_entries_no_update
BEFORE UPDATE ON journal_entries
FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'journal_entries is append-only; use a compensating entry';

CREATE TRIGGER trg_journal_entries_no_delete
BEFORE DELETE ON journal_entries
FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'journal_entries is append-only; use a compensating entry';

CREATE TRIGGER trg_postings_no_update
BEFORE UPDATE ON postings
FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'postings is append-only; use a compensating entry';

CREATE TRIGGER trg_postings_no_delete
BEFORE DELETE ON postings
FOR EACH ROW
SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT = 'postings is append-only; use a compensating entry';
