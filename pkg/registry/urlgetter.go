package registry

//
// Registers the `urlgetter' experiment.
//

import (
	"github.com/ooni/probe-engine/pkg/experiment/urlgetter"
	"github.com/ooni/probe-engine/pkg/model"
)

func init() {
	AllExperiments["urlgetter"] = &Factory{
		build: func(config interface{}) model.ExperimentMeasurer {
			return urlgetter.NewExperimentMeasurer(
				*config.(*urlgetter.Config),
			)
		},
		config:      &urlgetter.Config{},
		inputPolicy: model.InputStrictlyRequired,
	}
}