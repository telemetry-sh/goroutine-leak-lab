package sim

import (
	"reflect"
	"testing"
)

func TestRunIsDeterministic(t *testing.T) {
	first := Run(DefaultConfig())
	second := Run(DefaultConfig())
	if !reflect.DeepEqual(first, second) {
		t.Fatal("the same config and seed produced different simulations")
	}
}

func TestStrategiesExposeDifferentFailureModes(t *testing.T) {
	response := Run(DefaultConfig())
	results := indexResults(response.Strategies)

	detached := results[Detached].Metrics
	contextAware := results[ContextAware].Metrics
	buffered := results[Buffered].Metrics
	bounded := results[BoundedPool].Metrics

	if detached.FinalGoroutines < 500 || detached.BlockedGoroutines == 0 {
		t.Fatalf("detached workers should accumulate: %+v", detached)
	}
	if contextAware.FinalGoroutines != 0 || contextAware.CanceledWorkers != contextAware.TimedOut {
		t.Fatalf("context-aware workers should exit at cancellation: %+v", contextAware)
	}
	if buffered.PeakGoroutines >= detached.PeakGoroutines || buffered.PostTimeoutWorkMS == 0 {
		t.Fatalf("buffering should prevent permanent growth but retain late work: %+v", buffered)
	}
	if bounded.PeakGoroutines > response.Config.PoolSize {
		t.Fatalf("bounded pool exceeded its configured limit: %+v", bounded)
	}
	if bounded.Rejected == 0 && bounded.QueueP99MS == 0 {
		t.Fatalf("bounded pool should make overload visible: %+v", bounded)
	}
}

func TestAllStrategiesSeeTheSameRequestCohort(t *testing.T) {
	response := Run(DefaultConfig())
	expected := response.Config.RequestsPerSecond * response.Config.RunSeconds
	for _, result := range response.Strategies {
		if result.Metrics.Requests != expected {
			t.Fatalf("%s saw %d requests, want %d", result.Policy, result.Metrics.Requests, expected)
		}
		if result.Metrics.Completed+result.Metrics.TimedOut+result.Metrics.Rejected != expected {
			t.Fatalf("%s outcomes do not add up: %+v", result.Policy, result.Metrics)
		}
	}
}

func TestNormalizeClampsUntrustedInput(t *testing.T) {
	config := Normalize(Config{
		RequestsPerSecond: -10,
		TimeoutMS:         999999,
		SlowWorkPercent:   1000,
		SlowWorkMS:        -5,
		FastWorkMS:        99999,
		PoolSize:          -2,
		QueueSize:         999999,
		RunSeconds:        1,
	})

	if config.RequestsPerSecond != 5 || config.TimeoutMS != 2000 || config.SlowWorkPercent != 90 {
		t.Fatalf("unexpected normalized request controls: %+v", config)
	}
	if config.SlowWorkMS != 50 || config.FastWorkMS != 2000 {
		t.Fatalf("unexpected normalized work controls: %+v", config)
	}
	if config.PoolSize != 1 || config.QueueSize != 2000 || config.RunSeconds != 10 {
		t.Fatalf("unexpected normalized capacity controls: %+v", config)
	}
	if config.Seed == 0 {
		t.Fatal("zero seed should be replaced with a stable default")
	}
}

func indexResults(results []StrategyResult) map[Policy]StrategyResult {
	indexed := make(map[Policy]StrategyResult, len(results))
	for _, result := range results {
		indexed[result.Policy] = result
	}
	return indexed
}
