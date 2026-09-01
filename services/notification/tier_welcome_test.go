package notification

import (
	"os"
	"strings"
	"testing"

	"github.com/webtor-io/web-ui/services/i18n"
)

func newTierWelcomeService(t *testing.T, store *mockStore, mail *mockMailer) *Service {
	t.Helper()
	// The real template, not a stand-in: the test is about which lines the
	// data switches on and off.
	body, err := os.ReadFile("../../templates/notification/tier-welcome.html")
	if err != nil {
		t.Fatal(err)
	}
	svc := newTestService(store, mail, setupTemplateDir(t, map[string]string{"tier-welcome.html": string(body)}))
	// The real bundle too: an unresolved key in the rendered body is a
	// missing translation, which this test then catches for every line.
	svc.i18n = i18n.New(os.DirFS("../../locales"))
	return svc
}

func TestSendTierWelcome_KeyPerTierAndAllLines(t *testing.T) {
	store := &mockStore{}
	svc := newTierWelcomeService(t, store, &mockMailer{})

	err := svc.SendTierWelcome("user@example.com", testUserID, TierWelcome{
		Tier:        "silver",
		ShowStremio: true,
		ShowVault:   true,
		Billing:     Billing{Provider: "Patreon", ManageURL: "https://www.patreon.com/settings/memberships", TrialDays: 7},
	})
	if err != nil {
		t.Fatal(err)
	}
	if store.created == nil {
		t.Fatal("expected a feed entry")
	}
	if store.created.Key != "tier-welcome-silver" {
		t.Errorf("key: got %q", store.created.Key)
	}
	for _, want := range []string{
		"https://webtor.io/stremio/configure",
		"https://webtor.io/vault",
		"https://www.patreon.com/settings/memberships",
		"through Patreon",
		"free trial",
		"Silver",
	} {
		if !strings.Contains(store.created.Body, want) {
			t.Errorf("body lacks %q:\n%s", want, store.created.Body)
		}
	}
	if strings.Contains(store.created.Body, "email.tierWelcome.") {
		t.Errorf("unresolved translation key in body:\n%s", store.created.Body)
	}
	if !strings.Contains(store.created.Title, "Silver") || strings.Contains(store.created.Title, "email.") {
		t.Errorf("subject must name the tier in words, got %q", store.created.Title)
	}
}

// What the account already did, and what this deployment cannot say about
// billing, must not appear — a welcome that tells you to do what you did, or
// points at a provider that is not there, is noise.
func TestSendTierWelcome_OmitsDoneStepsAndAbsentBilling(t *testing.T) {
	store := &mockStore{}
	svc := newTierWelcomeService(t, store, &mockMailer{})

	err := svc.SendTierWelcome("user@example.com", testUserID, TierWelcome{Tier: "gold"})
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"/stremio/configure", "/vault", "Patreon", "free trial", "statement"} {
		if strings.Contains(store.created.Body, banned) {
			t.Errorf("body must not contain %q:\n%s", banned, store.created.Body)
		}
	}
	if !strings.Contains(store.created.Body, "Gold") {
		t.Errorf("heading must still name the tier:\n%s", store.created.Body)
	}
}

// Billing without a trial says where to manage the membership but nothing
// about trials — the trial sentence is only true where a trial exists.
func TestSendTierWelcome_BillingWithoutTrial(t *testing.T) {
	store := &mockStore{}
	svc := newTierWelcomeService(t, store, &mockMailer{})

	err := svc.SendTierWelcome("user@example.com", testUserID, TierWelcome{
		Tier:    "bronze",
		Billing: Billing{Provider: "Acme", ManageURL: "https://provider.example/manage"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(store.created.Body, "https://provider.example/manage") {
		t.Errorf("manage link missing:\n%s", store.created.Body)
	}
	if strings.Contains(store.created.Body, "free trial") {
		t.Errorf("trial sentence must not appear without a trial:\n%s", store.created.Body)
	}
}
