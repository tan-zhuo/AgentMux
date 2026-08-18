/**
 * The one rule the export UI has to know about before the backend sees it: the
 * shortest passphrase that will be accepted.
 *
 * It is kept in step with portable.MinPassphrase in the Go side by hand, which
 * is the honest arrangement — the backend enforces it, this only says it early
 * enough for the field to be answered rather than rejected.
 */
export const MIN_PASSPHRASE = 8
