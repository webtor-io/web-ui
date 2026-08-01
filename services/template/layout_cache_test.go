package template

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gin-contrib/multitemplate"
	"github.com/gin-gonic/gin"
)

// newLayoutTestManager builds a Manager over a single on-disk view whose
// "main" block echoes the data, which is enough to tell cache hits, misses
// and evictions apart by output alone.
func newLayoutTestManager(t *testing.T, capacity int) *Manager[thCtx] {
	t.Helper()
	gin.SetMode(gin.ReleaseMode)

	dir := t.TempDir()
	viewDir := filepath.Join(dir, "views")
	if err := os.MkdirAll(viewDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	viewPath := filepath.Join(viewDir, "page.html")
	if err := os.WriteFile(viewPath, []byte(`{{ define "main" }}page:{{ .V }}{{ end }}`), 0o644); err != nil {
		t.Fatalf("write view: %v", err)
	}

	m := &Manager[thCtx]{
		re:        multitemplate.New(),
		funcs:     FuncMap{},
		base:      dir + "/",
		layoutCap: capacity,
	}
	m.views = append(m.views, m.makeView(viewPath, "", nil, m.funcs))
	return m
}

// distinctBody returns semantically identical layout bodies that hash
// differently — the exact shape a header flood would take.
func distinctBody(i int) string {
	return fmt.Sprintf(`{{ template "main" . }}{{/* %d */}}`, i)
}

func TestLayoutBodyCacheIsBounded(t *testing.T) {
	const capacity = 4
	m := newLayoutTestManager(t, capacity)

	for i := 0; i < 200; i++ {
		if _, err := m.ExecuteLayoutBody("page", distinctBody(i), map[string]any{"V": "x"}); err != nil {
			t.Fatalf("execute %d: %v", i, err)
		}
	}

	if got := len(m.layoutCache); got != capacity {
		t.Fatalf("cache holds %d entries, want %d", got, capacity)
	}
	if got := m.layoutLRU.Len(); got != capacity {
		t.Fatalf("LRU holds %d entries, want %d", got, capacity)
	}
}

// The whole point of the fix: request-derived templates must never reach the
// shared multitemplate renderer, which gin reads unlocked on every c.HTML and
// from which nothing can be evicted safely.
func TestLayoutBodyNeverEntersSharedRenderer(t *testing.T) {
	m := newLayoutTestManager(t, 4)

	for i := 0; i < 50; i++ {
		if _, err := m.ExecuteLayoutBody("page", distinctBody(i), map[string]any{"V": "x"}); err != nil {
			t.Fatalf("execute %d: %v", i, err)
		}
	}

	shared, ok := m.re.(multitemplate.Render)
	if !ok {
		t.Fatalf("renderer is %T, expected multitemplate.Render in release mode", m.re)
	}
	if len(shared) != 0 {
		t.Fatalf("shared renderer gained %d entries, want 0", len(shared))
	}
}

func TestLayoutBodyRendersCorrectly(t *testing.T) {
	m := newLayoutTestManager(t, 4)

	for _, tc := range []struct{ body, want string }{
		{`{{ template "main" . }}`, "page:v1"},
		{`A{{ template "main" . }}Z`, "Apage:v1Z"},
		{`{{ template "main" . }}`, "page:v1"}, // cache hit must match the miss
	} {
		got, err := m.ExecuteLayoutBody("page", tc.body, map[string]any{"V": "v1"})
		if err != nil {
			t.Fatalf("execute %q: %v", tc.body, err)
		}
		if got != tc.want {
			t.Fatalf("body %q rendered %q, want %q", tc.body, got, tc.want)
		}
	}
}

// Eviction must not corrupt anything: a body pushed out of the LRU has to
// rebuild and render identically on its next use.
func TestEvictedLayoutRebuildsCorrectly(t *testing.T) {
	m := newLayoutTestManager(t, 2)

	bodies := []string{
		`1{{ template "main" . }}`,
		`2{{ template "main" . }}`,
		`3{{ template "main" . }}`,
		`4{{ template "main" . }}`,
	}
	for round := 0; round < 3; round++ {
		for i, b := range bodies {
			got, err := m.ExecuteLayoutBody("page", b, map[string]any{"V": "v"})
			if err != nil {
				t.Fatalf("round %d body %d: %v", round, i, err)
			}
			want := fmt.Sprintf("%dpage:v", i+1)
			if got != want {
				t.Fatalf("round %d: rendered %q, want %q", round, got, want)
			}
		}
	}
	if got := len(m.layoutCache); got > 2 {
		t.Fatalf("cache grew to %d past the cap of 2", got)
	}
}

func TestLayoutBodyUnknownViewErrors(t *testing.T) {
	m := newLayoutTestManager(t, 4)
	if _, err := m.ExecuteLayoutBody("nope", `{{ template "main" . }}`, nil); err == nil {
		t.Fatal("expected an error for an unregistered view name")
	}
}

// Run under -race: concurrent misses on the same key, concurrent evictions,
// and concurrent Execute on a shared *template.Template all happen here.
func TestLayoutBodyConcurrentAccess(t *testing.T) {
	m := newLayoutTestManager(t, 8)

	var wg sync.WaitGroup
	for g := 0; g < 24; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				// Mix a hot shared key with cold unique ones.
				body := `{{ template "main" . }}`
				if i%2 == 0 {
					body = distinctBody(g*40 + i)
				}
				got, err := m.ExecuteLayoutBody("page", body, map[string]any{"V": "c"})
				if err != nil {
					t.Errorf("goroutine %d iter %d: %v", g, i, err)
					return
				}
				if got != "page:c" {
					t.Errorf("goroutine %d iter %d rendered %q", g, i, got)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	if got := len(m.layoutCache); got > 8 {
		t.Fatalf("cache grew to %d past the cap of 8", got)
	}
	if len(m.layoutCache) != m.layoutLRU.Len() {
		t.Fatalf("map/LRU desync: map=%d lru=%d", len(m.layoutCache), m.layoutLRU.Len())
	}
}
