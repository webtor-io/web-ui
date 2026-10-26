package resource

import (
	"bytes"
	"html/template"
	"strings"
	"testing"
	"time"

	w "github.com/webtor-io/web-ui/services/web"
)

// The Save to Vault modal says what the click commits to before it, and a
// frozen pledge says when it can be removed instead of offering a Remove the
// server is certain to refuse. A paying user pledged 103.50 VP 2.7 s after
// opening the modal, and later met "come back later when the freeze period
// expires" seven times, with no time, before asking support how long it is
// (2026-10-26).

func vaultModal(t *testing.T) *template.Template {
	t.Helper()
	tpl, err := template.New("pledge-modal.html").Funcs(intentFuncs(t)).ParseFiles(
		"../../templates/partials/vault/pledge-modal.html",
		"../../templates/partials/vault/pledge-modal/add.html",
		"../../templates/partials/vault/pledge-modal/add/success.html",
		"../../templates/partials/vault/pledge-modal/remove.html",
		"../../templates/partials/vault/pledge-modal/freeze-note.html",
	)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return tpl
}

func renderModal(t *testing.T, tpl *template.Template, lang string, d *GetData) string {
	t.Helper()
	var b bytes.Buffer
	ctx := &w.Context{Lang: lang, Path: "/" + intentHash}
	if err := tpl.ExecuteTemplate(&b, "vault/pledge-modal", &w.CtxData{Ctx: ctx, Data: d}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	return b.String()
}

func f64(v float64) *float64 { return &v }

const nb = "\u00a0"

const (
	freezeNote   = "data-vault-freeze-note"
	storeForm    = `action="/vault/add"`
	removeAsk    = `name="pledge-remove-form"`
	removeForm   = `action="/vault/remove"`
	frozenLeft   = 13*time.Hour + 20*time.Minute
	freezePeriod = 24 * time.Hour
)

func addForm(available *float64) *VaultPledgeAddForm {
	return &VaultPledgeAddForm{Available: available, Total: f64(250), Required: 103.5, TorrentSizeGB: 103.5,
		ResourceID: intentHash, FreezePeriod: freezePeriod}
}

// S1: the line is there wherever the Store button is -- above it, after
// "What is Vault?" -- with the VP the pledge takes and the configured
// freeze; without the amount for unlimited points; gone with the button
// when the points do not cover the torrent, and gone when no freeze is
// configured (it would say "frozen for 1 min").
func TestVaultConfirmSaysTheFreezeBeforeTheClick(t *testing.T) {
	tpl := vaultModal(t)
	limited := "Saving freezes 103.50" + nb + "VP for 24" + nb + "h. After that you can remove the pledge at any time, and the points return to your balance."
	unlimited := "Once saved, the pledge is frozen for 24" + nb + "h. After that you can remove it at any time."

	enough := addForm(f64(250))
	out := renderModal(t, tpl, "en", &GetData{VaultPledgeAddForm: enough})
	note, store, whatIs := strings.Index(out, limited), strings.Index(out, storeForm), strings.Index(out, "/instructions/vault")
	if note < 0 || store < 0 {
		t.Fatalf("enough points: note at %d, Store at %d; want both:\n%s", note, store, out)
	}
	if !(whatIs < note && note < store) {
		t.Errorf("the note is not between What is Vault? and the Store button: %d, %d, %d", whatIs, note, store)
	}

	out = renderModal(t, tpl, "en", &GetData{VaultPledgeAddForm: addForm(nil)})
	if !strings.Contains(out, unlimited) || !strings.Contains(out, storeForm) || strings.Contains(out, "103.50"+nb+"VP") {
		t.Errorf("unlimited points: want the note without an amount and the Store button:\n%s", out)
	}

	out = renderModal(t, tpl, "en", &GetData{VaultPledgeAddForm: addForm(f64(50))})
	if strings.Contains(out, freezeNote) || strings.Contains(out, storeForm) {
		t.Errorf("not enough points: no Store button, so nothing to commit to; got a note or a button:\n%s", out)
	}

	none := addForm(f64(250))
	none.FreezePeriod = 0
	out = renderModal(t, tpl, "en", &GetData{VaultPledgeAddForm: none})
	if strings.Contains(out, freezeNote) || !strings.Contains(out, storeForm) {
		t.Errorf("no freeze configured: want the Store button and no note:\n%s", out)
	}
}

// S2, S3: the viewer's frozen pledge -- just saved, already saved, or the
// remove dialog opened from a page that offered Remove before -- says the
// time left and the rule, and offers no Remove; once the freeze is over,
// Remove is offered as before.
func TestVaultFrozenPledgeSaysWhenRemoveWorks(t *testing.T) {
	tpl := vaultModal(t)
	want := "This pledge is frozen for another 13" + nb + "h" + nb + "20" + nb + "min. Every pledge stays frozen for 24" + nb + "h after it is made. After that you can remove it here."

	for name, f := range map[string]*VaultPledgeAddForm{
		"just saved":    {Status: "success", FrozenFor: frozenLeft, FreezePeriod: freezePeriod, Funded: true},
		"already saved": {FrozenFor: frozenLeft, FreezePeriod: freezePeriod, Funded: true},
		"vaulted":       {FrozenFor: frozenLeft, FreezePeriod: freezePeriod, Funded: true, Vaulted: true},
	} {
		out := renderModal(t, tpl, "en", &GetData{VaultPledgeAddForm: f})
		if !strings.Contains(out, want) {
			t.Errorf("%s, frozen: no time left:\n%s", name, out)
		}
		if strings.Contains(out, removeAsk) {
			t.Errorf("%s, frozen: Remove Pledge is offered, and would be refused", name)
		}
		if !strings.Contains(out, `data-umami-event="vault-pledge-confirmed"`) {
			t.Errorf("%s, frozen: no Got it", name)
		}
	}

	out := renderModal(t, tpl, "en", &GetData{VaultPledgeAddForm: &VaultPledgeAddForm{Funded: true, FreezePeriod: freezePeriod}})
	if !strings.Contains(out, removeAsk) || strings.Contains(out, freezeNote) {
		t.Errorf("saved, not frozen: want Remove Pledge and no note:\n%s", out)
	}

	out = renderModal(t, tpl, "en", &GetData{VaultPledgeRemoveForm: &VaultPledgeRemoveForm{ResourceID: intentHash, FrozenFor: frozenLeft, FreezePeriod: freezePeriod}})
	if !strings.Contains(out, want) || strings.Contains(out, removeForm) {
		t.Errorf("remove dialog, frozen: want the time left and no Remove:\n%s", out)
	}
	if strings.Contains(out, "come back later") {
		t.Error("remove dialog, frozen: the old vague text is still there")
	}
	out = renderModal(t, tpl, "en", &GetData{VaultPledgeRemoveForm: &VaultPledgeRemoveForm{ResourceID: intentHash, FreezePeriod: freezePeriod}})
	if !strings.Contains(out, removeForm) || strings.Contains(out, freezeNote) {
		t.Errorf("remove dialog, not frozen: want the Remove form and no note:\n%s", out)
	}
}

// Just after the click the time left rounds up to the whole period, and the
// line read "frozen for another 24 h. Every pledge stays frozen for 24 h":
// then it says the period once, the short line -- in the saved view and in
// the remove dialog alike.
func TestVaultJustFrozenPledgeSaysThePeriodOnce(t *testing.T) {
	tpl := vaultModal(t)
	short := "Once saved, the pledge is frozen for 24" + nb + "h. After that you can remove it at any time."
	justNow := freezePeriod - 300*time.Millisecond
	for name, d := range map[string]*GetData{
		"just saved": {VaultPledgeAddForm: &VaultPledgeAddForm{Status: "success", FrozenFor: justNow, FreezePeriod: freezePeriod, Funded: true}},
		"remove":     {VaultPledgeRemoveForm: &VaultPledgeRemoveForm{ResourceID: intentHash, FrozenFor: justNow, FreezePeriod: freezePeriod}},
	} {
		out := renderModal(t, tpl, "en", d)
		if !strings.Contains(out, short) || strings.Count(out, "24"+nb+"h") != 1 {
			t.Errorf("%s, in the first minute: want the period once, in %q:\n%s", name, short, out)
		}
		if strings.Contains(out, removeAsk) || strings.Contains(out, removeForm) {
			t.Errorf("%s, in the first minute: Remove is offered, and would be refused", name)
		}
	}
}

// Every locale fills both lines' placeholders with its own durations: no
// "<no value>", no raw key, no English unit in a translated page, and no
// double stop where a dotted unit ends a sentence ("24 Std..").
func TestVaultFreezeLinesInEveryLocale(t *testing.T) {
	tpl := vaultModal(t)
	wh := &w.Helper{}
	for _, lang := range allLangs {
		left, period := wh.Duration(lang, frozenLeft), wh.Duration(lang, freezePeriod)
		for name, d := range map[string]*GetData{
			"confirm":   {VaultPledgeAddForm: addForm(f64(250))},
			"unlimited": {VaultPledgeAddForm: addForm(nil)},
			"saved":     {VaultPledgeAddForm: &VaultPledgeAddForm{FrozenFor: frozenLeft, FreezePeriod: freezePeriod, Funded: true}},
			"remove":    {VaultPledgeRemoveForm: &VaultPledgeRemoveForm{FrozenFor: frozenLeft, FreezePeriod: freezePeriod}},
		} {
			out := renderModal(t, tpl, lang, d)
			i := strings.Index(out, freezeNote)
			if i < 0 {
				t.Errorf("%s %s: no note", lang, name)
				continue
			}
			line := out[i : strings.Index(out[i:], "</p>")+i]
			line = line[strings.Index(line, "<span>"):]
			for _, bad := range []string{"<no value>", "vault.", "{{", ".."} {
				if strings.Contains(line, bad) {
					t.Errorf("%s %s: %q in %q", lang, name, bad, line)
				}
			}
			if !strings.Contains(line, period) {
				t.Errorf("%s %s: the period %q is not in %q", lang, name, period, line)
			}
			if (name == "saved" || name == "remove") && !strings.Contains(line, left) {
				t.Errorf("%s %s: the time left %q is not in %q", lang, name, left, line)
			}
			if name == "confirm" && !strings.Contains(line, "103.50"+nb+"VP") {
				t.Errorf("%s %s: the amount is not in %q", lang, name, line)
			}
		}
	}
}
