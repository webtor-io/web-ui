package web

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestBulkIDs(t *testing.T) {
	got, err := BulkIDs([]string{"a", " b ", "", "a", "c", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"a", "b", "c"}; !reflect.DeepEqual(got, want) {
		t.Errorf("BulkIDs = %q, want %q", got, want)
	}

	if got, _ := BulkIDs(nil); len(got) != 0 {
		t.Errorf("BulkIDs(nil) = %q, want empty", got)
	}

	ids := func(n int) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = strconv.Itoa(i)
		}
		return out
	}
	if got, err := BulkIDs(append(ids(MaxBulkIDs), "", "0")); err != nil || len(got) != MaxBulkIDs {
		t.Errorf("BulkIDs(%d distinct + blank + repeat) = %d, %v, want all accepted", MaxBulkIDs, len(got), err)
	}
	if _, err := BulkIDs(ids(MaxBulkIDs + 1)); err != ErrTooManyIDs {
		t.Errorf("BulkIDs(%d) = %v, want ErrTooManyIDs", MaxBulkIDs+1, err)
	}
}

// The selection arrives as one comma-joined field however large it is: a
// multipart part per id is cut off by Go's reader at 1000 parts, silently
// (gin swallows the error), which is why the form does not send it that way.
func TestFormBulkIDs(t *testing.T) {
	gin.SetMode(gin.TestMode)
	post := func(fields map[string][]string) []string {
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		for k, vs := range fields {
			for _, v := range vs {
				_ = mw.WriteField(k, v)
			}
		}
		_ = mw.Close()
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/", &body)
		c.Request.Header.Set("Content-Type", mw.FormDataContentType())
		got, err := FormBulkIDs(c)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := post(map[string][]string{"resource_id": {"a"}}); !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("single remove: %q", got)
	}
	many := make([]string, MaxBulkIDs)
	for i := range many {
		many[i] = strconv.Itoa(i)
	}
	if got := post(map[string][]string{"resource_ids": {strings.Join(many, ",")}}); len(got) != MaxBulkIDs {
		t.Errorf("one field of %d ids: got %d", MaxBulkIDs, len(got))
	}
}
