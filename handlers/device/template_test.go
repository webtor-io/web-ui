package device

import (
	"html/template"
	"os"
	"strings"
	"testing"
)

// The device view is a four-state machine and the rare states never render in
// a normal click-through, so each state is rendered standalone here. The
// confirm-state assertions pin two regressions at once: the missing _csrf
// hidden field (the 2026-08-10 "CSRF token mismatch" on /device/confirm) and
// the async-library wiring (data-async-target + the container's layout).
func renderState(t *testing.T, state string) string {
	t.Helper()
	src, err := os.ReadFile("../../templates/views/device/get.html")
	if err != nil {
		t.Fatal(err)
	}
	tpl := template.New("view").Funcs(template.FuncMap{
		"t":        func(lang, key string) string { return key },
		"langPath": func(lang, p string) string { return p },
	})
	if _, err := tpl.Parse(string(src)); err != nil {
		t.Fatalf("parse: %v", err)
	}
	var sb strings.Builder
	err = tpl.ExecuteTemplate(&sb, "main", map[string]any{
		"Lang": "en",
		"CSRF": "csrf-token-value",
		"Data": &Data{State: state, Code: "F7KQ-29XD", DeviceName: "webtor-cli @ host"},
	})
	if err != nil {
		t.Fatalf("execute state %q: %v", state, err)
	}
	return sb.String()
}

func TestConfirmStateCarriesCSRFAndAsyncTarget(t *testing.T) {
	out := renderState(t, "confirm")
	for _, want := range []string{
		`name="_csrf" value="csrf-token-value"`,
		`name="code" value="F7KQ-29XD"`,
		`data-async-target="#device-card"`,
		`id="device-card"`,
		`data-async-layout`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("confirm state lacks %s", want)
		}
	}
}

func TestAllStatesRender(t *testing.T) {
	for _, state := range []string{"input", "confirm", "done", "invalid"} {
		if out := renderState(t, state); len(out) < 100 {
			t.Errorf("state %q rendered suspiciously little output", state)
		}
	}
}
