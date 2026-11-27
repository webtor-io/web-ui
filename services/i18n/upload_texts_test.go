package i18n

import (
	"regexp"
	"strings"
	"testing"
)

// The uploader's messages say what is happening and what to do, never how
// the storage is built: a full node is "storage is full right now", not "the
// storage node is full". The node is the seeder that holds the files, an
// inside detail.
func TestUploadTextsNameNoStorageNode(t *testing.T) {
	node := regexp.MustCompile(`(?i)\bnode\b|узл|узел|knoten|\bnodo\b|nœud|węz[eł]|\bnó\P{L}|düğüm|\buzl|\buzel|节点`)
	locales := localeFiles(t)
	if len(locales) != 12 {
		t.Fatalf("%d locales, want 12", len(locales))
	}
	for lang, msgs := range locales {
		for k, v := range msgs {
			if strings.HasPrefix(k, "upload.") && node.MatchString(v) {
				t.Errorf("%s: %s names the node: %q", lang, k, v)
			}
		}
	}
}
