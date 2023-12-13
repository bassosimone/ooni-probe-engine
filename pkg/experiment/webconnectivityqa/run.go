package webconnectivityqa

import (
	"context"
	"fmt"
	"time"

	"github.com/apex/log"
	"github.com/ooni/probe-engine/pkg/logx"
	"github.com/ooni/probe-engine/pkg/model"
	"github.com/ooni/probe-engine/pkg/netemx"
	"github.com/ooni/probe-engine/pkg/netxlite"
)

// MeasureTestCase returns the JSON measurement produced by a [TestCase].
func MeasureTestCase(measurer model.ExperimentMeasurer, tc *TestCase) (*model.Measurement, error) {
	// configure the netemx scenario
	env := netemx.MustNewScenario(netemx.InternetScenario)
	defer env.Close()
	if tc.Configure != nil {
		tc.Configure(env)
	}

	// create the measurement skeleton
	t0 := time.Now().UTC()
	measurement := newMeasurement(tc.Input, measurer, t0)

	// create a logger for the probe
	prefixLogger := &logx.PrefixLogger{
		Prefix: fmt.Sprintf("%-16s", "PROBE"),
		Logger: log.Log,
	}

	var err error
	env.Do(func() {
		// create an HTTP client inside the env.Do function so we're using netem
		// TODO(https://github.com/ooni/probe/issues/2534): NewHTTPClientStdlib has QUIRKS
		// but they're not needed here
		httpClient := netxlite.NewHTTPClientStdlib(prefixLogger)
		arguments := &model.ExperimentArgs{
			Callbacks:   model.NewPrinterCallbacks(prefixLogger),
			Measurement: measurement,
			Session:     newSession(httpClient, prefixLogger),
		}

		// run the experiment
		ctx := context.Background()
		err = measurer.Run(ctx, arguments)

		// compute the total measurement runtime
		runtime := time.Since(t0)
		measurement.MeasurementRuntime = runtime.Seconds()
	})

	// handle the case of unexpected result
	switch {
	case err != nil && !tc.ExpectErr:
		return nil, fmt.Errorf("expected to see no error but got %s", err.Error())
	case err == nil && tc.ExpectErr:
		return nil, fmt.Errorf("expected to see an error but got <nil>")
	}

	return measurement, nil
}

// RunTestCase runs a [testCase].
func RunTestCase(measurer model.ExperimentMeasurer, tc *TestCase) error {
	measurement, err := MeasureTestCase(measurer, tc)
	if err != nil {
		return err
	}

	// reduce the test keys to a common format
	tk := newTestKeys(measurement)

	// compare the expected test keys to the ones we've got
	return compareTestKeys(tc.ExpectTestKeys, tk)
}
