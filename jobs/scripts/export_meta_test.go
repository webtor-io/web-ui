package scripts

import (
	"testing"

	ra "github.com/webtor-io/rest-api/services"
)

// rest-api omits the stream item for files it cannot stream and the zero
// item carries a nil Meta; reading it must yield a zero meta, not a panic.
func TestExportMeta_NilSafe(t *testing.T) {
	var absent ra.ExportItem
	if m := exportMeta(absent); m.Cache || m.Transcode {
		t.Errorf("zero item should give zero meta, got %+v", m)
	}
	present := ra.ExportItem{ExportMetaItem: ra.ExportMetaItem{Meta: &ra.ExportMeta{Cache: true, Transcode: true}}}
	if m := exportMeta(present); !m.Cache || !m.Transcode {
		t.Errorf("meta should pass through, got %+v", m)
	}
}
