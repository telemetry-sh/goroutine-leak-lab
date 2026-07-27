package sim

import (
	"fmt"
	"hash/fnv"
	"math"
	"math/rand"
	"slices"
)

type Policy string

const (
	Detached     Policy = "detached"
	ContextAware Policy = "context_aware"
	Buffered     Policy = "buffered"
	BoundedPool  Policy = "bounded_pool"
)

type Config struct {
	RequestsPerSecond int   `json:"requestsPerSecond"`
	TimeoutMS         int   `json:"timeoutMs"`
	SlowWorkPercent   int   `json:"slowWorkPercent"`
	SlowWorkMS        int   `json:"slowWorkMs"`
	FastWorkMS        int   `json:"fastWorkMs"`
	PoolSize          int   `json:"poolSize"`
	QueueSize         int   `json:"queueSize"`
	RunSeconds        int   `json:"runSeconds"`
	Seed              int64 `json:"seed"`
}

type Metrics struct {
	Requests          int     `json:"requests"`
	Completed         int     `json:"completed"`
	TimedOut          int     `json:"timedOut"`
	Rejected          int     `json:"rejected"`
	PeakGoroutines    int     `json:"peakGoroutines"`
	FinalGoroutines   int     `json:"finalGoroutines"`
	BlockedGoroutines int     `json:"blockedGoroutines"`
	CanceledWorkers   int     `json:"canceledWorkers"`
	PostTimeoutWorkMS int64   `json:"postTimeoutWorkMs"`
	P99LatencyMS      int     `json:"p99LatencyMs"`
	QueueP99MS        int     `json:"queueP99Ms"`
	SuccessPercent    float64 `json:"successPercent"`
}

type TimelinePoint struct {
	Second    int `json:"second"`
	Active    int `json:"active"`
	Blocked   int `json:"blocked"`
	Queued    int `json:"queued"`
	Completed int `json:"completed"`
	TimedOut  int `json:"timedOut"`
	Rejected  int `json:"rejected"`
}

type Event struct {
	TimestampMS     int64  `json:"timestampMs"`
	Policy          Policy `json:"policy"`
	RequestID       string `json:"requestId"`
	Outcome         string `json:"outcome"`
	GoroutineState  string `json:"goroutineState"`
	StackHash       string `json:"stackHash"`
	ContextCanceled bool   `json:"contextCanceled"`
	ResultDelivery  string `json:"resultDelivery"`
	PoolSlot        string `json:"poolSlot"`
	DurationMS      int    `json:"durationMs"`
}

type StrategyResult struct {
	Policy      Policy          `json:"policy"`
	Name        string          `json:"name"`
	Kicker      string          `json:"kicker"`
	Description string          `json:"description"`
	Tradeoff    string          `json:"tradeoff"`
	Color       string          `json:"color"`
	Recommended bool            `json:"recommended"`
	Metrics     Metrics         `json:"metrics"`
	Timeline    []TimelinePoint `json:"timeline"`
	Events      []Event         `json:"events"`
}

type Response struct {
	Config     Config           `json:"config"`
	Strategies []StrategyResult `json:"strategies"`
}

type request struct {
	id        int
	arrivalMS int
	workMS    int
}

type scheduledJob struct {
	request
	startMS      int
	completionMS int
	slot         int
}

func DefaultConfig() Config {
	return Config{
		RequestsPerSecond: 80,
		TimeoutMS:         120,
		SlowWorkPercent:   18,
		SlowWorkMS:        1600,
		FastWorkMS:        45,
		PoolSize:          24,
		QueueSize:         80,
		RunSeconds:        90,
		Seed:              6842,
	}
}

func Normalize(config Config) Config {
	defaults := DefaultConfig()
	if config.Seed == 0 {
		config.Seed = defaults.Seed
	}
	config.RequestsPerSecond = clampOrDefault(config.RequestsPerSecond, 5, 500, defaults.RequestsPerSecond)
	config.TimeoutMS = clampOrDefault(config.TimeoutMS, 20, 2000, defaults.TimeoutMS)
	config.SlowWorkPercent = clampOrDefault(config.SlowWorkPercent, 1, 90, defaults.SlowWorkPercent)
	config.SlowWorkMS = clampOrDefault(config.SlowWorkMS, 50, 10000, defaults.SlowWorkMS)
	config.FastWorkMS = clampOrDefault(config.FastWorkMS, 1, 2000, defaults.FastWorkMS)
	config.PoolSize = clampOrDefault(config.PoolSize, 1, 256, defaults.PoolSize)
	config.QueueSize = clampOrDefault(config.QueueSize, 0, 2000, defaults.QueueSize)
	config.RunSeconds = clampOrDefault(config.RunSeconds, 10, 300, defaults.RunSeconds)
	return config
}

func Run(config Config) Response {
	config = Normalize(config)
	requests := generateRequests(config)

	return Response{
		Config: config,
		Strategies: []StrategyResult{
			runDetached(config, requests),
			runContextAware(config, requests),
			runBuffered(config, requests),
			runBoundedPool(config, requests),
		},
	}
}

func generateRequests(config Config) []request {
	random := rand.New(rand.NewSource(config.Seed))
	requests := make([]request, 0, config.RequestsPerSecond*config.RunSeconds)
	id := 0

	for second := 0; second < config.RunSeconds; second++ {
		for index := 0; index < config.RequestsPerSecond; index++ {
			arrival := second*1000 + index*1000/config.RequestsPerSecond
			slow := random.Intn(100) < config.SlowWorkPercent
			work := config.FastWorkMS + random.Intn(max(2, config.FastWorkMS/3))
			if slow {
				jitter := max(2, config.SlowWorkMS/4)
				work = config.SlowWorkMS - jitter/2 + random.Intn(jitter)
			}
			requests = append(requests, request{id: id, arrivalMS: arrival, workMS: work})
			id++
		}
	}
	return requests
}

func runDetached(config Config, requests []request) StrategyResult {
	latencies := make([]int, 0, len(requests))
	timeline := baseTimeline(config.RunSeconds)
	events := make([]Event, 0, 6)
	var metrics Metrics

	for _, item := range requests {
		second := item.arrivalMS / 1000
		metrics.Requests++
		if item.workMS <= config.TimeoutMS {
			metrics.Completed++
			latencies = append(latencies, item.workMS)
			timeline[second].Completed++
			continue
		}

		metrics.TimedOut++
		metrics.PostTimeoutWorkMS += int64(item.workMS - config.TimeoutMS)
		latencies = append(latencies, config.TimeoutMS)
		timeline[second].TimedOut++
		for point := second; point < len(timeline); point++ {
			timeline[point].Active++
			if (point+1)*1000 >= item.arrivalMS+item.workMS {
				timeline[point].Blocked++
			}
		}
		if len(events) < cap(events) {
			events = append(events, makeEvent(item, Detached, config.TimeoutMS, "timeout", "chan send", false, "blocked: no receiver", "—"))
		}
	}

	metrics.FinalGoroutines = metrics.TimedOut
	metrics.PeakGoroutines = metrics.FinalGoroutines
	if len(timeline) > 0 {
		metrics.BlockedGoroutines = timeline[len(timeline)-1].Blocked
	}
	finishMetrics(&metrics, latencies, nil)
	return StrategyResult{
		Policy: Detached, Name: "Detached worker", Kicker: "handler returns · worker stays",
		Description: "The handler gives up at its deadline, but its worker ignores cancellation and later blocks sending to an abandoned result channel.",
		Tradeoff:    "Simple control flow hides a monotonically growing goroutine population.",
		Color:       "#ff6b5e", Metrics: metrics, Timeline: timeline, Events: events,
	}
}

func runContextAware(config Config, requests []request) StrategyResult {
	latencies := make([]int, 0, len(requests))
	timeline := baseTimeline(config.RunSeconds)
	events := make([]Event, 0, 6)
	var metrics Metrics

	for _, item := range requests {
		second := item.arrivalMS / 1000
		metrics.Requests++
		if item.workMS <= config.TimeoutMS {
			metrics.Completed++
			latencies = append(latencies, item.workMS)
			timeline[second].Completed++
			continue
		}

		metrics.TimedOut++
		metrics.CanceledWorkers++
		latencies = append(latencies, config.TimeoutMS)
		timeline[second].TimedOut++
		if len(events) < cap(events) {
			events = append(events, makeEvent(item, ContextAware, config.TimeoutMS, "timeout", "exited", true, "canceled before send", "—"))
		}
	}

	metrics.PeakGoroutines = peakConcurrency(requests, func(item request) int {
		return min(item.workMS, config.TimeoutMS)
	})
	finishMetrics(&metrics, latencies, nil)
	return StrategyResult{
		Policy: ContextAware, Name: "Context-aware", Kicker: "deadline propagates",
		Description: "The worker selects on ctx.Done(), stops the downstream call, and exits when the request deadline fires.",
		Tradeoff:    "Best lifecycle hygiene, but every dependency must honor cancellation.",
		Color:       "#b6f35b", Recommended: true, Metrics: metrics, Timeline: timeline, Events: events,
	}
}

func runBuffered(config Config, requests []request) StrategyResult {
	latencies := make([]int, 0, len(requests))
	timeline := baseTimeline(config.RunSeconds)
	events := make([]Event, 0, 6)
	endMS := config.RunSeconds * 1000
	var metrics Metrics

	for _, item := range requests {
		second := item.arrivalMS / 1000
		metrics.Requests++
		if item.workMS <= config.TimeoutMS {
			metrics.Completed++
			latencies = append(latencies, item.workMS)
			timeline[second].Completed++
			continue
		}

		completion := item.arrivalMS + item.workMS
		metrics.TimedOut++
		metrics.PostTimeoutWorkMS += int64(item.workMS - config.TimeoutMS)
		latencies = append(latencies, config.TimeoutMS)
		timeline[second].TimedOut++
		for point := second; point < len(timeline) && (point+1)*1000 < completion; point++ {
			timeline[point].Active++
		}
		if completion > endMS {
			metrics.FinalGoroutines++
		}
		if len(events) < cap(events) {
			events = append(events, makeEvent(item, Buffered, config.TimeoutMS, "timeout", "runnable", false, "buffer accepts late result", "—"))
		}
	}

	metrics.PeakGoroutines = peakConcurrency(requests, func(item request) int { return item.workMS })
	finishMetrics(&metrics, latencies, nil)
	return StrategyResult{
		Policy: Buffered, Name: "Buffered result", Kicker: "send unblocks · work survives",
		Description: "A one-slot result channel lets late workers exit instead of blocking forever, even after the handler has left.",
		Tradeoff:    "Prevents the send-side leak, but does not stop expensive post-timeout work.",
		Color:       "#5bd7ff", Metrics: metrics, Timeline: timeline, Events: events,
	}
}

func runBoundedPool(config Config, requests []request) StrategyResult {
	workerAvailable := make([]int, config.PoolSize)
	jobs := make([]scheduledJob, 0, len(requests))
	queueStarts := make([]int, 0, config.QueueSize+1)
	latencies := make([]int, 0, len(requests))
	queueWaits := make([]int, 0, len(requests))
	timeline := baseTimeline(config.RunSeconds)
	events := make([]Event, 0, 6)
	var metrics Metrics

	for _, item := range requests {
		second := item.arrivalMS / 1000
		metrics.Requests++
		queueStarts = discardStarted(queueStarts, item.arrivalMS)
		slot := earliestSlot(workerAvailable)
		start := max(item.arrivalMS, workerAvailable[slot])
		wait := start - item.arrivalMS

		if wait > 0 && len(queueStarts) >= config.QueueSize {
			metrics.Rejected++
			latencies = append(latencies, 1)
			timeline[second].Rejected++
			if len(events) < cap(events) {
				events = append(events, makeEvent(item, BoundedPool, 1, "rejected", "not started", false, "queue full", "—"))
			}
			continue
		}

		if wait > 0 {
			queueStarts = insertSorted(queueStarts, start)
		}
		completion := start + item.workMS
		workerAvailable[slot] = completion
		jobs = append(jobs, scheduledJob{request: item, startMS: start, completionMS: completion, slot: slot})
		queueWaits = append(queueWaits, wait)

		requestDuration := completion - item.arrivalMS
		if requestDuration <= config.TimeoutMS {
			metrics.Completed++
			latencies = append(latencies, requestDuration)
			timeline[second].Completed++
			continue
		}

		metrics.TimedOut++
		deadline := item.arrivalMS + config.TimeoutMS
		metrics.PostTimeoutWorkMS += int64(max(0, completion-max(start, deadline)))
		latencies = append(latencies, config.TimeoutMS)
		timeline[second].TimedOut++
		if len(events) < cap(events) {
			events = append(events, makeEvent(item, BoundedPool, config.TimeoutMS, "timeout", "pool worker", false, "worker remains bounded", fmt.Sprintf("%02d", slot)))
		}
	}

	endMS := config.RunSeconds * 1000
	for pointIndex := range timeline {
		at := (pointIndex + 1) * 1000
		for _, job := range jobs {
			if job.startMS <= at && job.completionMS > at {
				timeline[pointIndex].Active++
			}
			if job.startMS > at && job.arrivalMS <= at {
				timeline[pointIndex].Queued++
			}
		}
		metrics.PeakGoroutines = max(metrics.PeakGoroutines, timeline[pointIndex].Active)
	}
	for _, job := range jobs {
		if job.startMS <= endMS && job.completionMS > endMS {
			metrics.FinalGoroutines++
		}
	}
	finishMetrics(&metrics, latencies, queueWaits)
	return StrategyResult{
		Policy: BoundedPool, Name: "Bounded pool", Kicker: "growth capped · pressure visible",
		Description: "A fixed worker pool caps concurrency and turns runaway work into explicit queueing and rejection.",
		Tradeoff:    "Protects the process, but overload moves into queue delay and shed requests.",
		Color:       "#f7c95c", Metrics: metrics, Timeline: timeline, Events: events,
	}
}

func baseTimeline(seconds int) []TimelinePoint {
	points := make([]TimelinePoint, seconds)
	for index := range points {
		points[index].Second = index + 1
	}
	return points
}

func finishMetrics(metrics *Metrics, latencies, queueWaits []int) {
	metrics.P99LatencyMS = percentile(latencies, 0.99)
	metrics.QueueP99MS = percentile(queueWaits, 0.99)
	if metrics.Requests > 0 {
		metrics.SuccessPercent = math.Round(float64(metrics.Completed)*1000/float64(metrics.Requests)) / 10
	}
}

func peakConcurrency(requests []request, duration func(request) int) int {
	type change struct {
		at    int
		delta int
	}
	changes := make([]change, 0, len(requests)*2)
	for _, item := range requests {
		changes = append(changes,
			change{at: item.arrivalMS, delta: 1},
			change{at: item.arrivalMS + duration(item), delta: -1},
		)
	}
	slices.SortFunc(changes, func(left, right change) int {
		if left.at != right.at {
			return left.at - right.at
		}
		return left.delta - right.delta
	})
	current := 0
	peak := 0
	for _, item := range changes {
		current += item.delta
		peak = max(peak, current)
	}
	return peak
}

func percentile(values []int, quantile float64) int {
	if len(values) == 0 {
		return 0
	}
	copyOfValues := slices.Clone(values)
	slices.Sort(copyOfValues)
	index := int(math.Ceil(float64(len(copyOfValues))*quantile)) - 1
	return copyOfValues[max(0, min(index, len(copyOfValues)-1))]
}

func makeEvent(item request, policy Policy, duration int, outcome, state string, canceled bool, delivery, poolSlot string) Event {
	return Event{
		TimestampMS: int64(item.arrivalMS + duration), Policy: policy,
		RequestID: fmt.Sprintf("req_%05d", item.id), Outcome: outcome,
		GoroutineState: state, StackHash: stackHash(policy, state),
		ContextCanceled: canceled, ResultDelivery: delivery,
		PoolSlot: poolSlot, DurationMS: duration,
	}
}

func stackHash(policy Policy, state string) string {
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(string(policy) + ":" + state))
	return fmt.Sprintf("%08x", hash.Sum32())
}

func earliestSlot(available []int) int {
	selected := 0
	for index := 1; index < len(available); index++ {
		if available[index] < available[selected] {
			selected = index
		}
	}
	return selected
}

func discardStarted(starts []int, now int) []int {
	index := 0
	for index < len(starts) && starts[index] <= now {
		index++
	}
	return starts[index:]
}

func insertSorted(values []int, value int) []int {
	index, _ := slices.BinarySearch(values, value)
	values = append(values, 0)
	copy(values[index+1:], values[index:])
	values[index] = value
	return values
}

func clampOrDefault(value, minimum, maximum, fallback int) int {
	if value == 0 {
		return fallback
	}
	return min(max(value, minimum), maximum)
}
