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

// RecipientEmail picks the address an outgoing notification should actually
// go to: the confirmed notificationEmail when one is set and deliverable,
// falling back to email otherwise. This is the one place that decision is
// made -- every Send* call site should route through it rather than reading
// a user's Email field directly, so a self-hosted operator who has verified
// an address gets mail there, and a production user (who never has a
// notification address set at all, since the profile section that sets one
// never renders on webtor.io) keeps today's behaviour unchanged.
//
// Takes plain values rather than a *models.User or *auth.User so it has no
// opinion on which of this codebase's two user types the caller holds --
// both carry an Email string and a NotificationEmail *string, under those
// exact names, and call sites just pass the two fields through.
//
// notificationEmail is re-checked with Deliverable here even though it can
// currently only ever be populated (via models.VerifyPendingEmail) from a
// pending_email that notification.Deliverable already passed at submission
// time -- belt and suspenders against a future writer that skips that
// check, not a currently reachable case.
func RecipientEmail(email string, notificationEmail *string) string {
	if notificationEmail != nil && Deliverable(*notificationEmail) {
		return *notificationEmail
	}
	return email
}
