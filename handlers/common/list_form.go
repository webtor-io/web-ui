package common

import "strings"

// The profile has several drag-and-drop settings lists — Stremio addon URLs,
// Torznab indexers — that all submit the same three hidden fields: a
// comma-separated list of deleted ids, a comma-separated order, and one
// checkbox per row. The parsing of those fields lives here so the handlers
// only carry what differs between them.

// SplitIDs parses a comma-separated id field, dropping blanks. A field the
// form left empty yields nil rather than one empty id.
func SplitIDs(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ListOrder is a submitted row order, first row first.
type ListOrder []string

// NewListOrder parses the order field.
func NewListOrder(raw string) ListOrder {
	return SplitIDs(raw)
}

// Priority returns the priority value for a row and whether the order named
// it at all. Rows are ordered by priority DESC, so the first row gets the
// largest number — the convention every one of these lists already used.
func (o ListOrder) Priority(id string) (int16, bool) {
	for i, cur := range o {
		if cur == id {
			return int16(len(o) - i), true
		}
	}
	return 0, false
}
