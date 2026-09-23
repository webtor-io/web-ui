package j

import (
	"os"
	"strings"
	"testing"

	"github.com/pkg/errors"

	"github.com/webtor-io/web-ui/services/common"
	"github.com/webtor-io/web-ui/services/i18n"
	"github.com/webtor-io/web-ui/services/web"
)

// The job log renders a job's error in the visitor's language. A message with
// numbers (error.hash_length) gets them — rendered without, the count reads
// "<no value>".
func TestErrorFormatterQuotesTheNumbers(t *testing.T) {
	root, err := os.OpenRoot("../locales")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = root.Close() }()
	s := &Jobs{i18n: i18n.New(root.FS())}

	_, _, qerr := common.ResolveQueryHash("08ada5a7a6183aae1e09d831df6748d566095a10"[:39])
	msg := s.errorFormatter(&web.Context{Lang: "ru"})(errors.Wrap(qerr, "wrong resource provided"))
	if !strings.Contains(msg, "39 символов") || !strings.Contains(msg, "их 40") {
		t.Errorf("got %q, want the ru message with 39 and 40", msg)
	}
	msg = s.errorFormatter(&web.Context{Lang: "en"})(errors.Wrap(common.ErrQueryFreeText, "wrong resource provided"))
	if !strings.HasPrefix(msg, "Webtor doesn't search by title") {
		t.Errorf("a message without numbers: got %q", msg)
	}
}
