package api

import (
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/go-pg/pg/v10"
	uuid "github.com/satori/go.uuid"
	"github.com/webtor-io/web-ui/models"
	"github.com/webtor-io/web-ui/services/libapi"
)

// Device authorization endpoints (RFC 8628 shaped). Both are public — the
// device has no key yet, that is the point — so both sit outside the
// authorize group and behind their own limiters:
//
//   - code creation is limited per client IP, or an anonymous loop could
//     fill the table and burn through the user-code space;
//   - polling is limited per device_code at the advertised interval, and
//     answers slow_down exactly as the RFC prescribes.

// @Summary		Start device authorization
// @Description	For devices without a browser (CLI, TV): returns a `user_code` for the person and a secret
// @Description	`device_code` for the machine. Show `user_code` and send the person to `verification_uri`
// @Description	(or render `verification_uri_complete` as a QR code / link), then poll `POST /device/token`
// @Description	every `interval` seconds until the code expires or the key arrives.
// @Description
// @Description	No authentication — this is how a device obtains its key in the first place. The optional
// @Description	`name` labels the issued key in the profile's device list.
// @Tags			device
// @Accept			json
// @Produce		json
// @Param			request	body		libapi.DeviceCodeRequest	false	"Optional device label"
// @Success		200		{object}	libapi.DeviceCodeResponse
// @Failure		429		{object}	libapi.ErrorResponse	"Too many codes requested from this address"
// @Failure		500		{object}	libapi.ErrorResponse
// @Router			/device/code [post]
func (s *Handler) deviceCode(c *gin.Context) {
	if s.deviceCodeLimiter != nil {
		if retryAfter, ok := s.deviceCodeLimiter.Take("ip:" + c.ClientIP()); !ok {
			c.Header("Retry-After", strconv.Itoa(int(math.Ceil(retryAfter.Seconds()))))
			s.abort(c, libapi.NewError(http.StatusTooManyRequests, libapi.CodeRateLimited,
				"too many device codes requested — wait and retry", nil))
			return
		}
	}
	var req libapi.DeviceCodeRequest
	// The body is optional; a bare POST is fine.
	_ = c.ShouldBindJSON(&req)

	db := s.pg.Get()
	if db == nil {
		s.abort(c, libapi.NewError(http.StatusServiceUnavailable, libapi.CodeUnavailable, "database is not available", nil))
		return
	}

	var da *models.DeviceAuth
	// The code space is small by design; on the rare collision just draw again.
	for attempt := 0; attempt < 3; attempt++ {
		code, err := libapi.NewUserCode()
		if err != nil {
			s.abort(c, libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to generate code", err))
			return
		}
		da, err = models.CreateDeviceAuth(c.Request.Context(), db, code,
			libapi.SanitizeDeviceName(req.Name), libapi.DeviceCodeTTL)
		if err == nil {
			break
		}
		if pgErr, ok := err.(pg.Error); ok && pgErr.IntegrityViolation() {
			da = nil
			continue
		}
		s.abort(c, libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to create device authorization", err))
		return
	}
	if da == nil {
		s.abort(c, libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to allocate a user code", nil))
		return
	}

	verification := s.domain + libapi.DevicePath
	c.JSON(http.StatusOK, &libapi.DeviceCodeResponse{
		DeviceCode:              da.DeviceCode.String(),
		UserCode:                da.UserCode,
		VerificationURI:         verification,
		VerificationURIComplete: verification + "?code=" + da.UserCode,
		ExpiresIn:               libapi.DeviceCodeTTLSeconds,
		Interval:                libapi.DevicePollIntervalSeconds,
	})
}

// @Summary		Poll for the device key
// @Description	Poll with the `device_code` every `interval` seconds. Branch on the error `code`:
// @Description	`authorization_pending` (keep polling), `slow_down` (add a few seconds to the interval),
// @Description	`expired_token` (the person did not confirm in time — start over with `POST /device/code`).
// @Description	On success the response carries the key **exactly once**; store it, it is not retrievable again.
// @Tags			device
// @Accept			json
// @Produce		json
// @Param			request	body		libapi.DeviceTokenRequest	true	"The device_code from /device/code"
// @Success		200		{object}	libapi.DeviceTokenResponse
// @Failure		400		{object}	libapi.ErrorResponse	"authorization_pending, slow_down or expired_token"
// @Failure		500		{object}	libapi.ErrorResponse
// @Router			/device/token [post]
func (s *Handler) deviceToken(c *gin.Context) {
	var req libapi.DeviceTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.abort(c, libapi.NewError(http.StatusBadRequest, libapi.CodeBadRequest, "device_code is required", err))
		return
	}
	deviceCode, err := uuid.FromString(strings.TrimSpace(req.DeviceCode))
	if err != nil {
		s.abort(c, libapi.NewError(http.StatusBadRequest, libapi.CodeBadRequest, "device_code is not a valid code", err))
		return
	}
	// slow_down before any DB work: the limiter is the poll pacing itself.
	if s.devicePollLimiter != nil {
		if _, ok := s.devicePollLimiter.Take("poll:" + deviceCode.String()); !ok {
			s.abort(c, libapi.NewError(http.StatusBadRequest, libapi.CodeSlowDown,
				"polling faster than the advertised interval — slow down", nil))
			return
		}
	}
	db := s.pg.Get()
	if db == nil {
		s.abort(c, libapi.NewError(http.StatusServiceUnavailable, libapi.CodeUnavailable, "database is not available", nil))
		return
	}
	da, err := models.TakeDeviceAuth(c.Request.Context(), db, deviceCode)
	if err != nil {
		s.abort(c, libapi.NewError(http.StatusInternalServerError, libapi.CodeInternal, "failed to read device authorization", err))
		return
	}
	if da == nil {
		s.abort(c, libapi.NewError(http.StatusBadRequest, libapi.CodeExpiredToken,
			"unknown or expired device code — start over with POST /device/code", nil))
		return
	}
	if da.Status != models.DeviceAuthConfirmed || da.Token == nil {
		s.abort(c, libapi.NewError(http.StatusBadRequest, libapi.CodeAuthorizationPending,
			"waiting for the user to confirm the code", nil))
		return
	}
	c.JSON(http.StatusOK, &libapi.DeviceTokenResponse{Key: da.Token.String()})
}
