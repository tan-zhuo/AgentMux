package orch

// TruncateForTest exposes the internal truncation to the package's external
// tests. It is a test seam rather than API: how a result is shortened is the
// engine's business, but that it never mangles a character is worth pinning.
func TruncateForTest(s string, max int) string { return truncate(s, max) }
