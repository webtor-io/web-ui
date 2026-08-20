package claims

import "testing"

// NewClient deliberately returns nil when no provider is configured -- the
// self-hosted case. Callers then hold a nil *Client and defer Close on it, and
// most of them guard that with a nil check; subscription.go did not, so
// `web-ui subscription poll` finished its work and then died in the deferred
// teardown with a segfault. Make the method itself tolerate the nil the
// constructor hands out, so no caller has to remember.
func TestCloseToleratesTheNilConstructorReturns(t *testing.T) {
	var c *Client
	c.Close()
}
