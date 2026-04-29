package api

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func newTestApi(t *testing.T) *Api {
	t.Helper()
	return &Api{secret: "test-secret"}
}

func TestSignClaims_Roundtrip(t *testing.T) {
	a := newTestApi(t)
	tok, err := a.SignClaims(GraceClaims{Rate: "50M", Role: "grace", Hash: "hash123", Kind: "grace"})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jwt.Parse(tok, func(*jwt.Token) (interface{}, error) { return []byte(a.secret), nil })
	if err != nil || !parsed.Valid {
		t.Fatalf("token did not validate: %v", err)
	}
	mc := parsed.Claims.(jwt.MapClaims)
	if mc["hash"] != "hash123" || mc["kind"] != "grace" || mc["rate"] != "50M" {
		t.Errorf("unexpected claims: %+v", mc)
	}
}

func TestSignClaims_NoSecret(t *testing.T) {
	a := &Api{}
	if _, err := a.SignClaims(GraceClaims{Hash: "h"}); err == nil {
		t.Error("expected error when secret is empty")
	}
}

// TestClaimsWithRules_Roundtrip — primary Claims with Rules signs cleanly and
// the inner grace token survives the JSON round-trip. This is what rest-api
// will receive in X-Token and copy verbatim into export URL tokens.
func TestClaimsWithRules_Roundtrip(t *testing.T) {
	a := newTestApi(t)
	graceTok, err := a.SignClaims(GraceClaims{Rate: "50M", Role: "grace", Hash: "h", Kind: "grace"})
	if err != nil {
		t.Fatal(err)
	}
	cl := Claims{
		Rate: "5M",
		Role: "free",
		Rules: []Rule{{
			Kind:        "grace",
			Scope:       "manifest",
			DurationSec: 1200,
			Token:       graceTok,
		}},
	}
	tok, err := a.SignClaims(cl)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := jwt.Parse(tok, func(*jwt.Token) (interface{}, error) { return []byte(a.secret), nil })
	if err != nil || !parsed.Valid {
		t.Fatalf("token did not validate: %v", err)
	}
	mc := parsed.Claims.(jwt.MapClaims)
	if mc["rate"] != "5M" || mc["role"] != "free" {
		t.Errorf("primary claims wrong: %+v", mc)
	}
	rulesArr, ok := mc["rules"].([]interface{})
	if !ok || len(rulesArr) != 1 {
		t.Fatalf("rules missing or wrong shape: %+v", mc["rules"])
	}
	r := rulesArr[0].(map[string]interface{})
	if r["kind"] != "grace" || r["scope"] != "manifest" || r["duration_sec"].(float64) != 1200 {
		t.Errorf("rule fields wrong: %+v", r)
	}
	if r["token"] != graceTok {
		t.Errorf("inner grace token did not survive: got=%v want=%v", r["token"], graceTok)
	}
}

// TestClaimsWithoutRules_NoRulesField — without grace, the "rules" field must
// be omitted entirely (not emitted as null/empty array). Avoids surprising
// downstream consumers that don't know about rules yet.
func TestClaimsWithoutRules_NoRulesField(t *testing.T) {
	a := newTestApi(t)
	cl := Claims{Rate: "5M", Role: "free"}
	tok, err := a.SignClaims(cl)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := jwt.Parse(tok, func(*jwt.Token) (interface{}, error) { return []byte(a.secret), nil })
	mc := parsed.Claims.(jwt.MapClaims)
	if _, present := mc["rules"]; present {
		t.Errorf("rules field must be omitted when empty: %+v", mc)
	}
	if _, present := mc["hash"]; present {
		t.Errorf("hash field must be omitted when empty: %+v", mc)
	}
}

// TestClaimsWithHash_BindsToInfohash — when Hash is set on primary claims,
// it survives signing and shows up as the top-level "hash" claim that THP's
// generic boundHash check reads.
func TestClaimsWithHash_BindsToInfohash(t *testing.T) {
	a := newTestApi(t)
	cl := Claims{Rate: "5M", Role: "free", Hash: "ca307c54b7e18f214d9355dfa07a5ec70c6e1a58"}
	tok, err := a.SignClaims(cl)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := jwt.Parse(tok, func(*jwt.Token) (interface{}, error) { return []byte(a.secret), nil })
	mc := parsed.Claims.(jwt.MapClaims)
	if mc["hash"] != "ca307c54b7e18f214d9355dfa07a5ec70c6e1a58" {
		t.Errorf("hash claim missing or wrong: %+v", mc)
	}
}
