package uploads

import (
	"context"
	"net/http"
	"net/url"

	"github.com/pkg/errors"

	"github.com/webtor-io/web-ui/services/api"
)

// ErrGone answers Seal for an upload whose directory the seeder no longer
// has.
var ErrGone = errors.New("upload is gone")

// Delete deletes the upload id on the seeder. tombstoned: the seeder answered
// 204, it wrote the upload's tombstone and the upload can never grow back.
// 410 is no error either, but says less: the directory is not there, whether
// as a tombstone or because the upload's home changed. Any other answer, 404
// included, leaves the files where they were and is an error.
func (s *Service) Delete(ctx context.Context, id string) (tombstoned bool, err error) {
	status, err := s.call(ctx, http.MethodDelete, id)
	switch {
	case err != nil:
		return false, errors.Wrap(err, "upload delete")
	case status == http.StatusNoContent:
		return true, nil
	case status == http.StatusGone:
		return false, nil
	}
	return false, errors.Errorf("upload delete: status %d", status)
}

// Seal closes the upload id for writes on the seeder: after it the seeder
// takes no more of its files. ErrGone: the directory is not there.
func (s *Service) Seal(ctx context.Context, id string) error {
	status, err := s.call(ctx, http.MethodPut, id)
	switch {
	case err != nil:
		return errors.Wrap(err, "upload seal")
	case status == http.StatusNoContent:
		return nil
	case status == http.StatusGone:
		return ErrGone
	}
	return errors.Errorf("upload seal: status %d", status)
}

// call sends web-ui's service request method for the upload id and answers
// the seeder's status. Every failure on the way, the URL's parse included,
// is a *url.Error that quotes the URL with its token and key: only its cause
// is kept.
func (s *Service) call(ctx context.Context, method, id string) (int, error) {
	status, err := s.send(ctx, method, id)
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	return status, err
}

func (s *Service) send(ctx context.Context, method, id string) (int, error) {
	u, err := s.serviceURL(id)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return 0, err
	}
	res, err := s.cl.Do(req)
	if err != nil {
		return 0, err
	}
	_ = res.Body.Close()
	return res.StatusCode, nil
}

// serviceURL is the upload id's directory on the seeder with a service
// token: web-ui's own, bound to id, the way the seeder takes a whole
// upload's delete and close. UPLOAD_URL is the browser's address, which the
// server side may not reach: the request goes the way web-ui's other
// server-side requests to torrent-http-proxy go.
func (s *Service) serviceURL(id string) (string, error) {
	c := api.InternalClaims()
	c.Hash, c.Up, c.Lim = id, "add", 1
	token, err := s.api.SignClaims(c)
	if err != nil {
		return "", errors.Wrap(err, "failed to sign a service token")
	}
	q := url.Values{"token": {token}, "api-key": {s.key}}
	return s.api.ProxyURL(s.url + "/" + id + "/?" + q.Encode())
}
