package s3

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	awss3 "github.com/aws/aws-sdk-go/service/s3"
	"github.com/webtor-io/web-ui/services/vfs"
)

const (
	testUser      = "11111111-2222-3333-4444-555555555555"
	testAccessKey = "99999999-8888-7777-6666-555555555555"
	testSecret    = "signing-secret"
	movieDir      = "Movie One (2020)"
	// The shape that broke listings: a literal "+" among the spaces.
	plusDir = "Some Release (Deluxe + Bonus) [2024]"
)

var testModTime = time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)

// fakeFS is an in-memory tree shaped like the real library: four roots, a
// torrent folder with nested files, and .torrent files under /torrents.
type fakeFS struct {
	dirs     map[string]bool
	files    map[string]int64
	redirect string
	body     string
}

func newFakeFS() *fakeFS {
	return &fakeFS{
		dirs: map[string]bool{
			"/":                           true,
			"/all/":                       true,
			"/movies/":                    true,
			"/series/":                    true,
			"/torrents/":                  true,
			"/all/" + movieDir + "/":      true,
			"/all/" + movieDir + "/subs/": true,
			"/all/Другой Фильм/":          true,
			"/all/" + plusDir + "/":       true,
		},
		files: map[string]int64{
			"/all/" + movieDir + "/video.mkv":    1024,
			"/all/" + movieDir + "/subs/eng.srt": 12,
			"/all/Другой Фильм/video.mkv":        2048,
			"/all/" + plusDir + "/setup.iso":     4096,
			"/torrents/" + movieDir + ".torrent": 64,
		},
	}
}

func (f *fakeFS) Stat(_ context.Context, name string) (*vfs.FileInfo, error) {
	if f.dirs[name] || f.dirs[name+"/"] {
		p := strings.TrimSuffix(name, "/") + "/"
		if name == "/" {
			p = "/"
		}
		return &vfs.FileInfo{Path: p, IsDir: true, ModTime: testModTime}, nil
	}
	if size, ok := f.files[name]; ok {
		return &vfs.FileInfo{Path: name, Size: size, ModTime: testModTime, MIMEType: "video/x-matroska"}, nil
	}
	return nil, vfs.NewHTTPError(http.StatusNotFound, nil)
}

func (f *fakeFS) ReadDir(_ context.Context, name string, _ bool) ([]vfs.FileInfo, error) {
	if !f.dirs[name] && !f.dirs[name+"/"] {
		return nil, vfs.NewHTTPError(http.StatusNotFound, nil)
	}
	dir := strings.TrimSuffix(name, "/") + "/"
	var out []vfs.FileInfo
	for d := range f.dirs {
		if d == dir || !strings.HasPrefix(d, dir) {
			continue
		}
		if strings.Count(strings.TrimPrefix(d, dir), "/") != 1 {
			continue
		}
		out = append(out, vfs.FileInfo{Path: d, IsDir: true, ModTime: testModTime})
	}
	for p, size := range f.files {
		if !strings.HasPrefix(p, dir) || strings.Contains(strings.TrimPrefix(p, dir), "/") {
			continue
		}
		out = append(out, vfs.FileInfo{Path: p, Size: size, ModTime: testModTime, MIMEType: "video/x-matroska"})
	}
	return out, nil
}

func (f *fakeFS) Open(_ context.Context, name string) (io.ReadCloser, *url.URL, error) {
	if _, ok := f.files[name]; !ok {
		return nil, nil, vfs.NewHTTPError(http.StatusNotFound, nil)
	}
	if strings.HasPrefix(name, "/torrents/") {
		return io.NopCloser(strings.NewReader(f.body)), nil, nil
	}
	u, _ := url.Parse(f.redirect)
	return nil, u, nil
}

func (f *fakeFS) Create(context.Context, string, io.ReadCloser, *vfs.CreateOptions) (*vfs.FileInfo, bool, error) {
	return nil, false, vfs.NewHTTPError(http.StatusForbidden, nil)
}

// RemoveAll mirrors the real tree: only the torrents bucket is writable, and a
// missing entry is a 404 from the filesystem's point of view.
func (f *fakeFS) RemoveAll(_ context.Context, name string, _ *vfs.RemoveAllOptions) error {
	if !strings.HasPrefix(name, "/torrents/") {
		return vfs.NewHTTPError(http.StatusForbidden, nil)
	}
	if _, ok := f.files[name]; !ok {
		return vfs.NewHTTPError(http.StatusNotFound, nil)
	}
	delete(f.files, name)
	return nil
}
func (f *fakeFS) Mkdir(context.Context, string) error {
	return vfs.NewHTTPError(http.StatusForbidden, nil)
}
func (f *fakeFS) Copy(context.Context, string, string, *vfs.CopyOptions) (bool, error) {
	return false, vfs.NewHTTPError(http.StatusForbidden, nil)
}
func (f *fakeFS) Move(context.Context, string, string, *vfs.MoveOptions) (bool, error) {
	return false, vfs.NewHTTPError(http.StatusForbidden, nil)
}

var _ vfs.FileSystem = (*fakeFS)(nil)

func newTestServer(t *testing.T, fs vfs.FileSystem) *httptest.Server {
	t.Helper()
	h := New(fs, testSecret, "/s3")
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(WithUser(r.Context(), testUser))
		h.ServeHTTP(w, r)
	}))
}

func newClient(t *testing.T, endpoint string, secret string) *awss3.S3 {
	t.Helper()
	sess, err := session.NewSession(&aws.Config{
		Credentials:      credentials.NewStaticCredentials(testAccessKey, secret, ""),
		Endpoint:         aws.String(endpoint + "/s3"),
		Region:           aws.String("us-east-1"),
		S3ForcePathStyle: aws.Bool(true),
	})
	if err != nil {
		t.Fatal(err)
	}
	return awss3.New(sess)
}

func newSignedClient(t *testing.T, endpoint string) *awss3.S3 {
	return newClient(t, endpoint, DeriveSecretKey(testSecret, testAccessKey))
}

func TestListBuckets(t *testing.T) {
	srv := newTestServer(t, newFakeFS())
	defer srv.Close()

	out, err := newSignedClient(t, srv.URL).ListBuckets(&awss3.ListBucketsInput{})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, b := range out.Buckets {
		got[aws.StringValue(b.Name)] = true
	}
	for _, want := range []string{"all", "movies", "series", "torrents"} {
		if !got[want] {
			t.Errorf("bucket %q missing from %v", want, got)
		}
	}
}

// A wrong secret must be rejected, otherwise the access key would be a bearer
// token and the signature decorative.
func TestSignatureMismatchIsRejected(t *testing.T) {
	srv := newTestServer(t, newFakeFS())
	defer srv.Close()

	_, err := newClient(t, srv.URL, "not-the-derived-secret").ListBuckets(&awss3.ListBucketsInput{})
	if err == nil {
		t.Fatal("expected an error for a bad secret")
	}
	var aerr awserr.RequestFailure
	if !errorsAs(err, &aerr) {
		t.Fatalf("expected a request failure, got %v", err)
	}
	if aerr.Code() != ErrCodeSignatureMismatch {
		t.Errorf("got code %q, want %q", aerr.Code(), ErrCodeSignatureMismatch)
	}
}

func TestListObjectsWithDelimiter(t *testing.T) {
	srv := newTestServer(t, newFakeFS())
	defer srv.Close()

	out, err := newSignedClient(t, srv.URL).ListObjectsV2(&awss3.ListObjectsV2Input{
		Bucket:    aws.String("all"),
		Delimiter: aws.String("/"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Contents) != 0 {
		t.Errorf("expected no objects at the bucket root, got %v", out.Contents)
	}
	var prefixes []string
	for _, p := range out.CommonPrefixes {
		prefixes = append(prefixes, aws.StringValue(p.Prefix))
	}
	if len(prefixes) != 3 {
		t.Fatalf("expected three folders, got %v", prefixes)
	}

	out, err = newSignedClient(t, srv.URL).ListObjectsV2(&awss3.ListObjectsV2Input{
		Bucket:    aws.String("all"),
		Delimiter: aws.String("/"),
		Prefix:    aws.String(movieDir + "/"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Contents) != 1 || aws.StringValue(out.Contents[0].Key) != movieDir+"/video.mkv" {
		t.Errorf("unexpected contents: %v", out.Contents)
	}
	if len(out.CommonPrefixes) != 1 || aws.StringValue(out.CommonPrefixes[0].Prefix) != movieDir+"/subs/" {
		t.Errorf("unexpected prefixes: %v", out.CommonPrefixes)
	}
}

// No delimiter means a recursive walk — the path `rclone sync` and
// `aws s3 ls --recursive` take.
func TestListObjectsRecursive(t *testing.T) {
	srv := newTestServer(t, newFakeFS())
	defer srv.Close()

	out, err := newSignedClient(t, srv.URL).ListObjectsV2(&awss3.ListObjectsV2Input{
		Bucket: aws.String("all"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, o := range out.Contents {
		keys = append(keys, aws.StringValue(o.Key))
	}
	want := []string{movieDir + "/subs/eng.srt", movieDir + "/video.mkv", plusDir + "/setup.iso", "Другой Фильм/video.mkv"}
	if len(keys) != len(want) {
		t.Fatalf("got %v, want %v", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Errorf("key %d: got %q, want %q (listing must be sorted)", i, keys[i], want[i])
		}
	}
}

func TestListObjectsPagination(t *testing.T) {
	srv := newTestServer(t, newFakeFS())
	defer srv.Close()
	cl := newSignedClient(t, srv.URL)

	first, err := cl.ListObjectsV2(&awss3.ListObjectsV2Input{
		Bucket:  aws.String("all"),
		MaxKeys: aws.Int64(1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !aws.BoolValue(first.IsTruncated) {
		t.Fatal("expected a truncated first page")
	}
	if aws.StringValue(first.NextContinuationToken) == "" {
		t.Fatal("expected a continuation token")
	}

	var all []string
	for _, o := range first.Contents {
		all = append(all, aws.StringValue(o.Key))
	}
	token := first.NextContinuationToken
	for i := 0; i < 20 && token != nil && aws.StringValue(token) != ""; i++ {
		page, err := cl.ListObjectsV2(&awss3.ListObjectsV2Input{
			Bucket:            aws.String("all"),
			MaxKeys:           aws.Int64(1),
			ContinuationToken: token,
		})
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range page.Contents {
			all = append(all, aws.StringValue(o.Key))
		}
		if !aws.BoolValue(page.IsTruncated) {
			token = nil
			break
		}
		token = page.NextContinuationToken
	}
	if len(all) != 4 {
		t.Errorf("paged listing returned %v, want every key", all)
	}
}

func TestHeadObject(t *testing.T) {
	srv := newTestServer(t, newFakeFS())
	defer srv.Close()
	cl := newSignedClient(t, srv.URL)

	out, err := cl.HeadObject(&awss3.HeadObjectInput{
		Bucket: aws.String("all"),
		Key:    aws.String(movieDir + "/video.mkv"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if aws.Int64Value(out.ContentLength) != 1024 {
		t.Errorf("got size %d, want 1024", aws.Int64Value(out.ContentLength))
	}

	// A folder is not an object: S3 has no directories, and a client probing
	// one expects a miss.
	if _, err := cl.HeadObject(&awss3.HeadObjectInput{
		Bucket: aws.String("all"),
		Key:    aws.String(movieDir),
	}); err == nil {
		t.Error("expected a miss for a directory key")
	}
}

// The ETag must never look like an MD5: rclone verifies downloads against a
// 32-hex ETag and would fail every transfer, since we never hash content.
func TestETagIsNotAnMD5(t *testing.T) {
	srv := newTestServer(t, newFakeFS())
	defer srv.Close()

	out, err := newSignedClient(t, srv.URL).ListObjectsV2(&awss3.ListObjectsV2Input{
		Bucket: aws.String("all"),
	})
	if err != nil {
		t.Fatal(err)
	}
	md5Like := regexp.MustCompile(`^[0-9a-f]{32}$`)
	for _, o := range out.Contents {
		etag := strings.Trim(aws.StringValue(o.ETag), `"`)
		if etag == "" {
			t.Errorf("empty etag for %s", aws.StringValue(o.Key))
		}
		if md5Like.MatchString(etag) {
			t.Errorf("etag %q for %s looks like an MD5", etag, aws.StringValue(o.Key))
		}
	}
}

// GetObject answers with a redirect to the streaming chain. This test is the
// standing check that an S3 client actually follows it instead of surfacing the
// 307 as an error.
func TestGetObjectFollowsRedirect(t *testing.T) {
	content := "the movie bytes"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, content)
	}))
	defer origin.Close()

	fs := newFakeFS()
	fs.redirect = origin.URL + "/video.mkv"
	srv := newTestServer(t, fs)
	defer srv.Close()

	out, err := newSignedClient(t, srv.URL).GetObject(&awss3.GetObjectInput{
		Bucket: aws.String("all"),
		Key:    aws.String(movieDir + "/video.mkv"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = out.Body.Close()
	}()
	got, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != content {
		t.Errorf("got %q, want %q", got, content)
	}
}

// .torrent files are small and local, so they are streamed rather than
// redirected — the same split WebDAV makes.
func TestGetObjectStreamsTorrent(t *testing.T) {
	fs := newFakeFS()
	fs.body = "d8:announce"
	srv := newTestServer(t, fs)
	defer srv.Close()

	out, err := newSignedClient(t, srv.URL).GetObject(&awss3.GetObjectInput{
		Bucket: aws.String("torrents"),
		Key:    aws.String(movieDir + ".torrent"),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = out.Body.Close()
	}()
	got, _ := io.ReadAll(out.Body)
	if string(got) != fs.body {
		t.Errorf("got %q, want %q", got, fs.body)
	}
}

func TestHeadBucket(t *testing.T) {
	srv := newTestServer(t, newFakeFS())
	defer srv.Close()
	cl := newSignedClient(t, srv.URL)

	if _, err := cl.HeadBucket(&awss3.HeadBucketInput{Bucket: aws.String("movies")}); err != nil {
		t.Fatalf("head bucket failed: %v", err)
	}
	if _, err := cl.HeadBucket(&awss3.HeadBucketInput{Bucket: aws.String("nope")}); err == nil {
		t.Error("expected an error for an unknown bucket")
	}
}

func TestWritesAreRejectedOnReadOnlyBuckets(t *testing.T) {
	srv := newTestServer(t, newFakeFS())
	defer srv.Close()

	_, err := newSignedClient(t, srv.URL).PutObject(&awss3.PutObjectInput{
		Bucket: aws.String("all"),
		Key:    aws.String("whatever.mkv"),
		Body:   strings.NewReader("x"),
	})
	if err == nil {
		t.Fatal("expected access denied")
	}
	var aerr awserr.RequestFailure
	if !errorsAs(err, &aerr) || aerr.Code() != ErrCodeAccessDenied {
		t.Errorf("got %v, want AccessDenied", err)
	}
}

// A recursive listing that would fan out into an unbounded number of rest-api
// calls must fail loudly: a short answer that claims to be complete would make
// `rclone sync` delete the missing files on the other side.
func TestRecursiveListingCapFailsLoudly(t *testing.T) {
	fs := newFakeFS()
	h := New(fs, testSecret, "/s3")
	h.MaxWalkDirs = 1
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(WithUser(r.Context(), testUser))
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	_, err := newSignedClient(t, srv.URL).ListObjectsV2(&awss3.ListObjectsV2Input{Bucket: aws.String("all")})
	if err == nil {
		t.Fatal("expected the listing to fail")
	}
	var aerr awserr.RequestFailure
	if !errorsAs(err, &aerr) || aerr.Code() != ErrCodeInvalidRequest {
		t.Errorf("got %v, want InvalidRequest", err)
	}
}

// encoding-type=url is what keeps names with spaces, brackets or Cyrillic from
// coming back mangled: the client percent-decodes whatever we send, so we have
// to encode it, and echo EncodingType so it knows to.
func TestListObjectsURLEncoding(t *testing.T) {
	srv := newTestServer(t, newFakeFS())
	defer srv.Close()

	out, err := newSignedClient(t, srv.URL).ListObjectsV2(&awss3.ListObjectsV2Input{
		Bucket:       aws.String("all"),
		Delimiter:    aws.String("/"),
		EncodingType: aws.String("url"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if aws.StringValue(out.EncodingType) != "url" {
		t.Errorf("encoding type not echoed: %q", aws.StringValue(out.EncodingType))
	}
	var decoded []string
	for _, p := range out.CommonPrefixes {
		// The SDK hands the raw value over; clients undo the encoding
		// themselves with QueryUnescape, so that is what we verify against.
		d, err := url.QueryUnescape(aws.StringValue(p.Prefix))
		if err != nil {
			t.Fatalf("prefix %q does not decode: %v", aws.StringValue(p.Prefix), err)
		}
		decoded = append(decoded, d)
	}
	want := map[string]bool{movieDir + "/": true, "Другой Фильм/": true, plusDir + "/": true}
	for _, d := range decoded {
		if !want[d] {
			t.Errorf("unexpected prefix %q (decoded from the listing)", d)
		}
		delete(want, d)
	}
	if len(want) != 0 {
		t.Errorf("missing prefixes: %v", want)
	}

	// The same encoding applies to what the client sends back as a prefix.
	out, err = newSignedClient(t, srv.URL).ListObjectsV2(&awss3.ListObjectsV2Input{
		Bucket:       aws.String("all"),
		Delimiter:    aws.String("/"),
		EncodingType: aws.String("url"),
		Prefix:       aws.String("Другой Фильм/"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Contents) != 1 {
		t.Fatalf("expected one object, got %v", out.Contents)
	}
	key, _ := url.QueryUnescape(aws.StringValue(out.Contents[0].Key))
	if key != "Другой Фильм/video.mkv" {
		t.Errorf("got key %q", key)
	}
}

// A literal "+" in a name is where form-encoding and percent-encoding disagree,
// and it broke real listings: the response encoded spaces as "+", and the prefix
// coming back was decoded a second time, turning the name's own "+" into a
// space. Neither side may use form encoding.
func TestPlusInNamesSurvivesListing(t *testing.T) {
	srv := newTestServer(t, newFakeFS())
	defer srv.Close()
	cl := newSignedClient(t, srv.URL)

	out, err := cl.ListObjectsV2(&awss3.ListObjectsV2Input{
		Bucket:       aws.String("all"),
		Delimiter:    aws.String("/"),
		EncodingType: aws.String("url"),
	})
	if err != nil {
		t.Fatal(err)
	}
	var encoded string
	for _, p := range out.CommonPrefixes {
		// Match on the decoded value: the encoded form is what is under test.
		if d, err := url.PathUnescape(aws.StringValue(p.Prefix)); err == nil && d == plusDir+"/" {
			encoded = aws.StringValue(p.Prefix)
		}
	}
	if encoded == "" {
		t.Fatal("the folder is missing from the listing")
	}
	if strings.Contains(encoded, "+") {
		t.Errorf("%q uses form encoding: a client that percent-decodes reads those as literal plusses", encoded)
	}
	// A percent-only decoder — the strict reading, and what Cyberduck does —
	// has to recover the name exactly.
	if got, err := url.PathUnescape(encoded); err != nil || got != plusDir+"/" {
		t.Errorf("percent-decoded to %q (err %v), want %q", got, err, plusDir+"/")
	}

	// And the client can descend into it: the prefix it sends must not be
	// decoded twice.
	inner, err := cl.ListObjectsV2(&awss3.ListObjectsV2Input{
		Bucket:       aws.String("all"),
		Delimiter:    aws.String("/"),
		EncodingType: aws.String("url"),
		Prefix:       aws.String(plusDir + "/"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(inner.Contents) != 1 {
		t.Fatalf("listing inside the folder returned %v", inner.Contents)
	}
	key, _ := url.PathUnescape(aws.StringValue(inner.Contents[0].Key))
	if key != plusDir+"/setup.iso" {
		t.Errorf("got key %q", key)
	}

	// The same name has to work without encoding-type too.
	plain, err := cl.ListObjectsV2(&awss3.ListObjectsV2Input{
		Bucket:    aws.String("all"),
		Delimiter: aws.String("/"),
		Prefix:    aws.String(plusDir + "/"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.Contents) != 1 || aws.StringValue(plain.Contents[0].Key) != plusDir+"/setup.iso" {
		t.Errorf("unencoded listing returned %v", plain.Contents)
	}
}

// Query-string (presigned) signatures share the verification path with header
// ones, and they are what a shareable link would be built from.
func TestPresignedGet(t *testing.T) {
	content := "presigned bytes"
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, content)
	}))
	defer origin.Close()

	fs := newFakeFS()
	fs.redirect = origin.URL + "/video.mkv"
	srv := newTestServer(t, fs)
	defer srv.Close()

	req, _ := newSignedClient(t, srv.URL).GetObjectRequest(&awss3.GetObjectInput{
		Bucket: aws.String("all"),
		Key:    aws.String(movieDir + "/video.mkv"),
	})
	signed, err := req.Presign(5 * time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Get(signed)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || string(body) != content {
		t.Errorf("got %d %q", resp.StatusCode, body)
	}
}

// DeleteObject is idempotent in S3. It matters here beyond spec-compliance: a
// client-side move is copy-then-delete, our copy renames the source away, and
// the delete that follows would otherwise fail the whole operation.
func TestDeleteObjectIsIdempotent(t *testing.T) {
	srv := newTestServer(t, newFakeFS())
	defer srv.Close()
	cl := newSignedClient(t, srv.URL)

	for _, pass := range []string{"first", "second"} {
		if _, err := cl.DeleteObject(&awss3.DeleteObjectInput{
			Bucket: aws.String("torrents"),
			Key:    aws.String(movieDir + ".torrent"),
		}); err != nil {
			t.Fatalf("%s delete failed: %v", pass, err)
		}
	}

	// Read-only buckets still refuse.
	if _, err := cl.DeleteObject(&awss3.DeleteObjectInput{
		Bucket: aws.String("all"),
		Key:    aws.String(movieDir + "/video.mkv"),
	}); err == nil {
		t.Error("expected a delete on a read-only bucket to fail")
	}
}

// Keys are concatenated into a filesystem path, so dot segments are refused
// rather than resolved.
func TestDotSegmentsRejected(t *testing.T) {
	srv := newTestServer(t, newFakeFS())
	defer srv.Close()

	_, err := newSignedClient(t, srv.URL).HeadObject(&awss3.HeadObjectInput{
		Bucket: aws.String("all"),
		Key:    aws.String("../torrents/x.torrent"),
	})
	if err == nil {
		t.Fatal("expected the key to be rejected")
	}
}

// An empty v2 listing has to state KeyCount explicitly.
func TestEmptyListingReportsKeyCount(t *testing.T) {
	srv := newTestServer(t, newFakeFS())
	defer srv.Close()

	out, err := newSignedClient(t, srv.URL).ListObjectsV2(&awss3.ListObjectsV2Input{
		Bucket:    aws.String("movies"),
		Delimiter: aws.String("/"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.KeyCount == nil || aws.Int64Value(out.KeyCount) != 0 {
		t.Errorf("got KeyCount %v, want 0", out.KeyCount)
	}
	if aws.BoolValue(out.IsTruncated) {
		t.Error("an empty listing must not be truncated")
	}
}

func errorsAs(err error, target *awserr.RequestFailure) bool {
	if rf, ok := err.(awserr.RequestFailure); ok {
		*target = rf
		return true
	}
	return false
}
