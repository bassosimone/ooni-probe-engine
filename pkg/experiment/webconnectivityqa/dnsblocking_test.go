package webconnectivityqa

import (
	"context"
	"errors"
	"testing"

	"github.com/apex/log"
	"github.com/ooni/probe-engine/pkg/netemx"
	"github.com/ooni/probe-engine/pkg/netxlite"
)

func TestDNSBlockingAndroidDNSCacheNoData(t *testing.T) {
	env := netemx.MustNewScenario(netemx.InternetScenario)
	tc := dnsBlockingAndroidDNSCacheNoData()
	tc.Configure(env)

	env.Do(func() {
		reso := netxlite.NewStdlibResolver(log.Log)
		addrs, err := reso.LookupHost(context.Background(), "www.example.com")
		if !errors.Is(err, netxlite.ErrAndroidDNSCacheNoData) {
			t.Fatal("unexpected error", err)
		}
		if len(addrs) != 0 {
			t.Fatal("expected to see no addresses")
		}
	})
}

func TestDNSBlockingNXDOMAIN(t *testing.T) {
	env := netemx.MustNewScenario(netemx.InternetScenario)
	tc := dnsBlockingNXDOMAIN()
	tc.Configure(env)

	env.Do(func() {
		reso := netxlite.NewStdlibResolver(log.Log)
		addrs, err := reso.LookupHost(context.Background(), "www.example.com")
		if err == nil || err.Error() != netxlite.FailureDNSNXDOMAINError {
			t.Fatal("unexpected error", err)
		}
		if len(addrs) != 0 {
			t.Fatal("expected to see no addresses")
		}
	})
}
