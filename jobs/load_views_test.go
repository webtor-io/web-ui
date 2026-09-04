package j

import (
	"strings"
	"testing"

	filepathx "github.com/yargevad/filepathx"
)

// Jobs render their own cards, so Jobs.New registers "load/**/*" on the
// template manager. The pattern must actually reach the files: the first
// version of the magnet card was never registered, tb.Build failed and the
// user saw the generic red line after a perfect countdown.
func TestLoadErrorViewsAreDiscoverable(t *testing.T) {
	files, err := filepathx.Glob("../templates/views/load/**/*")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range files {
		if strings.HasSuffix(f, "load/errors/magnet.html") {
			found = true
		}
	}
	if !found {
		t.Fatalf("load/**/* does not reach load/errors/magnet.html; got %v", files)
	}
}
