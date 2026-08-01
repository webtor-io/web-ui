package s3

import (
	"bufio"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/pkg/errors"
)

// decodeBody unwraps the aws-chunked transfer encoding the AWS CLI and SDKs use
// when they sign a payload incrementally (x-amz-content-sha256:
// STREAMING-AWS4-HMAC-SHA256-PAYLOAD). Without this the stored .torrent would
// have chunk headers baked into it.
//
// The per-chunk signatures are not verified: the request itself is signed and
// the only writable target is a .torrent that has to parse as a valid metainfo
// anyway. Verifying the chunk chain is the follow-up if we ever accept large or
// arbitrary uploads here.
func decodeBody(r *http.Request) (io.ReadCloser, *Error) {
	if !strings.HasPrefix(r.Header.Get(amzContentSHA256), "STREAMING-") {
		return r.Body, nil
	}
	return io.NopCloser(&chunkedReader{r: bufio.NewReader(r.Body)}), nil
}

type chunkedReader struct {
	r    *bufio.Reader
	rest int64
	done bool
	err  error
}

func (c *chunkedReader) Read(p []byte) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	if c.done {
		return 0, io.EOF
	}
	if c.rest == 0 {
		n, err := c.readChunkHeader()
		if err != nil {
			c.err = err
			return 0, err
		}
		if n == 0 {
			c.done = true
			return 0, io.EOF
		}
		c.rest = n
	}
	if int64(len(p)) > c.rest {
		p = p[:c.rest]
	}
	n, err := c.r.Read(p)
	c.rest -= int64(n)
	if c.rest == 0 && err == nil {
		// Consume the CRLF that terminates the chunk payload.
		_, err = c.r.Discard(2)
	}
	if err != nil {
		c.err = err
	}
	return n, err
}

// readChunkHeader parses "<hex-size>;chunk-signature=<sig>\r\n".
func (c *chunkedReader) readChunkHeader() (int64, error) {
	line, err := c.r.ReadString('\n')
	if err != nil {
		return 0, errors.Wrap(err, "failed to read chunk header")
	}
	line = strings.TrimRight(line, "\r\n")
	sizePart, _, _ := strings.Cut(line, ";")
	size, err := strconv.ParseInt(strings.TrimSpace(sizePart), 16, 64)
	if err != nil {
		return 0, errors.Wrapf(err, "failed to parse chunk size %q", sizePart)
	}
	return size, nil
}
