package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/pkg/errors"
)

// privateTrackersPath is rest-api's internal endpoint of a user's private
// tracker records, and the label of its errors: the full path holds the uid,
// which never reaches an error text.
const privateTrackersPath = "/internal/private-trackers"

// PrivateTrackerRecord is one private tracker record of a user, as rest-api's
// internal endpoint answers it: hosts only, no URLs and no keys.
type PrivateTrackerRecord struct {
	ResourceID string    `json:"resource_id"`
	Hosts      []string  `json:"hosts"`
	Source     string    `json:"source"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// ListPrivateTrackers reads every private tracker record of uid, for the
// account export.
func (s *Api) ListPrivateTrackers(ctx context.Context, uid string) ([]PrivateTrackerRecord, error) {
	var records []PrivateTrackerRecord
	if err := s.privateTrackers(ctx, http.MethodGet, uid, &records); err != nil {
		return nil, err
	}
	return records, nil
}

// DeletePrivateTrackers deletes every private tracker record of uid, before
// the account is deleted, and returns how many there were.
func (s *Api) DeletePrivateTrackers(ctx context.Context, uid string) (int, error) {
	var res struct {
		Deleted int `json:"deleted"`
	}
	if err := s.privateTrackers(ctx, http.MethodDelete, uid, &res); err != nil {
		return 0, err
	}
	return res.Deleted, nil
}

// privateTrackers sends method to uid's records with web-ui's own claims and
// reads a 200 into v. private_unavailable, at any status, is a store without
// private records: v stays empty. Any other answer, a 404 too, is an error:
// an old rest-api without the endpoint must not read as "no records". Through
// RapidAPI nothing is sent: the gateway has no /internal/ and one uid serves
// every web-ui user.
func (s *Api) privateTrackers(ctx context.Context, method, uid string, v any) error {
	if s.rapidAPI {
		return nil
	}
	res, err := s.doRequestRaw(ctx, InternalClaims(), s.url+privateTrackersPath+"/"+url.PathEscape(uid), method, nil)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			ue.URL = privateTrackersPath
		}
		return err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(res.Body)
	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode == http.StatusOK {
		return json.Unmarshal(body, v)
	}
	err = errorFromResponse(res, body, privateTrackersPath)
	var pe *PrivateError
	if errors.As(err, &pe) && pe.Code == CodePrivateUnavailable {
		return nil
	}
	return err
}
