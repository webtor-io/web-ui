package notification

import "strings"

// Deliverable reports whether mail sent to this address could plausibly
// arrive. It is deliberately not "the field is non-empty": public."user"
// declares email NOT NULL UNIQUE, so an absent address never looks like an
// empty string, and the self-hosted admin account carries the literal
// sentinel "admin" (services/adminauth/pg_repo.go). Every guard written as
// Email == "" passes that sentinel straight through and mails it.
//
// This is a syntactic check, not proof of delivery: a well-formed address
// can still bounce. Its job is to catch the placeholders this system
// creates for itself, not to validate the internet.
func Deliverable(email string) bool {
	e := strings.TrimSpace(email)
	at := strings.LastIndex(e, "@")
	if at <= 0 || at == len(e)-1 {
		return false
	}
	domain := e[at+1:]
	dot := strings.Index(domain, ".")
	return dot > 0 && dot < len(domain)-1
}
