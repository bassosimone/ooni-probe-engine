package webconnectivityqa

import (
	"net/http"
	"strings"
	"testing"

	"github.com/apex/log"
	"github.com/ooni/probe-engine/pkg/netemx"
	"github.com/ooni/probe-engine/pkg/netxlite"
)

func TestHTTPBlockingConnectionReset(t *testing.T) {
	env := netemx.MustNewScenario(netemx.InternetScenario)
	defer env.Close()

	tc := httpBlockingConnectionReset()
	tc.Configure(env)

	env.Do(func() {
		dialer := netxlite.NewDialerWithStdlibResolver(log.Log)
		tlsDialer := netxlite.NewTLSDialer(dialer, netxlite.NewTLSHandshakerStdlib(log.Log))
		txp := netxlite.NewHTTPTransportWithOptions(log.Log, dialer, tlsDialer)
		client := &http.Client{Transport: txp}
		resp, err := client.Get("http://www.example.com/")
		if err == nil || !strings.HasSuffix(err.Error(), "connection_reset") {
			t.Fatal("unexpected err", err)
		}
		if resp != nil {
			t.Fatal("expected nil resp")
		}
	})
}
