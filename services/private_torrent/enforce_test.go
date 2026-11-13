package private_torrent

import (
	"flag"
	"testing"

	"github.com/urfave/cli"

	"github.com/webtor-io/web-ui/services/metrics/metricstest"
)

func configure(t *testing.T, args ...string) {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, f := range RegisterFlags(nil) {
		f.Apply(fs)
	}
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	Configure(cli.NewContext(cli.NewApp(), fs, nil))
}

// The switch is off unless the flag is given, and the gauge says which way
// it stands.
func TestEnforceSwitch(t *testing.T) {
	t.Cleanup(func() { SetEnforce(false) })

	configure(t)
	if Enforce() {
		t.Error("Enforce() = true without the flag")
	}
	if g := metricstest.Gauge(t, "webui_private_enforce"); g != 0 {
		t.Errorf("webui_private_enforce = %v without the flag, want 0", g)
	}

	configure(t, "--"+EnforceFlag)
	if !Enforce() {
		t.Error("Enforce() = false with --" + EnforceFlag)
	}
	if g := metricstest.Gauge(t, "webui_private_enforce"); g != 1 {
		t.Errorf("webui_private_enforce = %v with --%s, want 1", g, EnforceFlag)
	}
}
