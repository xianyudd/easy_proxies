package quality

import (
	"context"
	"strings"
	"sync"
)

type workerConfig struct {
	workers         int
	kind            CheckKind
	expectedCountry string
	quick           QuickRunner
	cf              CloudflareRunner
	rep             ReputationRunner
}

func runTargets(ctx context.Context, cfg workerConfig, targets []Target, emit func(Result) bool, progress func(completed, failed int) bool) error {
	if cfg.workers <= 0 {
		cfg.workers = 1
	}
	if cfg.workers > len(targets) && len(targets) > 0 {
		cfg.workers = len(targets)
	}
	if len(targets) == 0 {
		return nil
	}

	jobs := make(chan Target)
	var wg sync.WaitGroup
	var mu sync.Mutex
	completed := 0
	failed := 0
	stop := false

	mark := func(result Result) bool {
		mu.Lock()
		defer mu.Unlock()
		if stop {
			return false
		}
		if result.Error != "" || result.Status == "failed" || !result.Success {
			failed++
		}
		completed++
		if emit != nil && !emit(result) {
			stop = true
			return false
		}
		if progress != nil && !progress(completed, failed) {
			stop = true
			return false
		}
		return true
	}

	for i := 0; i < cfg.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case target, ok := <-jobs:
					if !ok {
						return
					}
					result := runOneTarget(ctx, cfg, target)
					if !mark(result) {
						return
					}
				}
			}
		}()
	}

sendLoop:
	for _, target := range targets {
		mu.Lock()
		stopped := stop
		mu.Unlock()
		if stopped {
			break sendLoop
		}
		select {
		case <-ctx.Done():
			break sendLoop
		case jobs <- target:
		}
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	mu.Lock()
	stopped := stop
	mu.Unlock()
	if stopped {
		return context.Canceled
	}
	return nil
}

func runOneTarget(ctx context.Context, cfg workerConfig, target Target) Result {
	base := Result{Kind: cfg.kind, Target: target, TargetIndex: target.Index, TargetID: target.ID, NodeName: target.NodeName, NodeTag: target.NodeTag, Source: target.Source, ProxyURL: target.ProxyURL, Protocol: target.Protocol, Host: target.Host, Port: target.Port, Region: target.Region}
	select {
	case <-ctx.Done():
		base.Error = ctx.Err().Error()
		base.Status = "failed"
		return base
	default:
	}
	switch cfg.kind {
	case CheckQuick:
		if cfg.quick == nil {
			base.Error = "quick runner is not configured"
			base.Status = "failed"
			return base
		}
		result := cfg.quick.CheckQuick(ctx, target)
		return mergeBaseResult(base, result)
	case CheckCloudflare:
		if cfg.cf == nil {
			base.Error = "cloudflare runner is not configured"
			base.Status = "failed"
			return base
		}
		result := cfg.cf.CheckCloudflare(ctx, target)
		return mergeBaseResult(base, result)
	case CheckReputation:
		if cfg.rep == nil {
			base.Error = "reputation runner is not configured"
			base.Status = "failed"
			return base
		}
		result := cfg.rep.CheckReputation(ctx, target, cfg.expectedCountry)
		return mergeBaseResult(base, result)
	case CheckCombined:
		if cfg.cf == nil || cfg.rep == nil {
			base.Error = "quality runners are not configured"
			base.Status = "failed"
			return base
		}
		cf := mergeBaseResult(Result{Kind: CheckCloudflare, Target: target, TargetIndex: target.Index, TargetID: target.ID}, cfg.cf.CheckCloudflare(ctx, target))
		if err := ctx.Err(); err != nil {
			base.Error = err.Error()
			base.Status = "failed"
			return base
		}
		rep := mergeBaseResult(Result{Kind: CheckReputation, Target: target, TargetIndex: target.Index, TargetID: target.ID}, cfg.rep.CheckReputation(ctx, target, cfg.expectedCountry))
		base.Kind = CheckCombined
		base.Success = cf.Error == "" && rep.Error == "" && cf.Success && rep.Success
		base.Status = "completed"
		if !base.Success {
			base.Status = "failed"
			base.Error = firstError(cf.Error, rep.Error)
		}
		base.Score = cf.Score
		base.LatencyMS = cf.LatencyMS + rep.LatencyMS
		base.CF = resultMap(cf)
		base.Reputation = resultMap(rep)
		return base
	case CheckPipeline:
		if cfg.quick == nil || cfg.cf == nil || cfg.rep == nil {
			base.Error = "pipeline runners are not configured"
			base.Status = "failed"
			return base
		}
		quick := mergeBaseResult(Result{Kind: CheckQuick, Target: target, TargetIndex: target.Index, TargetID: target.ID}, cfg.quick.CheckQuick(ctx, target))
		base.Kind = CheckPipeline
		base.Quick = resultMap(quick)
		base.LatencyMS = quick.LatencyMS
		if quick.Error != "" || quick.Status == "failed" || !quick.Success {
			base.Success = false
			base.Status = "failed"
			base.Error = quick.Error
			if base.Error == "" {
				base.Error = "quick check failed"
			}
			base.FinalScore = 0
			base.Recommend = false
			return base
		}
		if err := ctx.Err(); err != nil {
			base.Error = err.Error()
			base.Status = "failed"
			return base
		}
		cf := mergeBaseResult(Result{Kind: CheckCloudflare, Target: target, TargetIndex: target.Index, TargetID: target.ID}, cfg.cf.CheckCloudflare(ctx, target))
		if err := ctx.Err(); err != nil {
			base.Error = err.Error()
			base.Status = "failed"
			return base
		}
		rep := mergeBaseResult(Result{Kind: CheckReputation, Target: target, TargetIndex: target.Index, TargetID: target.ID}, cfg.rep.CheckReputation(ctx, target, cfg.expectedCountry))
		base.Success = cf.Error == "" && rep.Error == "" && cf.Success && rep.Success
		base.Status = "completed"
		if !base.Success {
			base.Status = "failed"
			base.Error = firstError(cf.Error, rep.Error)
		}
		base.Score = cf.Score
		base.LatencyMS += cf.LatencyMS + rep.LatencyMS
		base.CF = resultMap(cf)
		base.Reputation = resultMap(rep)
		base.FinalScore = finalScore(cf, rep, base.LatencyMS)
		base.Recommend = base.Success && base.FinalScore >= 75
		return base
	default:
		base.Error = "invalid quality job kind"
		base.Status = "failed"
		return base
	}
}

func mergeBaseResult(base, result Result) Result {
	if result.Kind == "" {
		result.Kind = base.Kind
	}
	if result.TargetIndex == 0 {
		result.TargetIndex = base.TargetIndex
	}
	if result.TargetID == "" {
		result.TargetID = base.TargetID
	}
	if result.Target.ID == "" {
		result.Target = base.Target
	}
	if result.NodeName == "" {
		result.NodeName = base.NodeName
	}
	if result.NodeTag == "" {
		result.NodeTag = base.NodeTag
	}
	if result.Source == "" {
		result.Source = base.Source
	}
	if result.ProxyURL == "" {
		result.ProxyURL = base.ProxyURL
	}
	if result.Protocol == "" {
		result.Protocol = base.Protocol
	}
	if result.Host == "" {
		result.Host = base.Host
	}
	if result.Port == 0 {
		result.Port = base.Port
	}
	if result.Region == "" {
		result.Region = base.Region
	}
	if result.Status == "" {
		if result.Error != "" || !result.Success {
			result.Status = "failed"
		} else {
			result.Status = "completed"
		}
	}
	return result
}

func resultMap(result Result) map[string]any {
	out := cloneAnyMap(result.Quick)
	if len(out) == 0 {
		out = cloneAnyMap(result.CF)
	}
	if len(out) == 0 {
		out = cloneAnyMap(result.Reputation)
	}
	if out == nil {
		out = make(map[string]any)
	}
	out["success"] = result.Success
	out["status"] = result.Status
	if result.Score != 0 {
		out["score"] = result.Score
	}
	if result.LatencyMS != 0 {
		out["latency_ms"] = result.LatencyMS
	}
	if result.Error != "" {
		out["error"] = result.Error
	}
	return out
}

func finalScore(cf, rep Result, latencyMS int64) int {
	score := cf.Score
	if score == 0 && cf.Success {
		score = 70
	}
	risk := repSummaryLevel(rep)
	switch risk {
	case "medium":
		score -= 15
	case "high":
		score -= 40
	case "failed":
		score -= 50
	}
	switch {
	case latencyMS > 3000:
		score -= 25
	case latencyMS > 1000:
		score -= 10
	}
	if cf.Error != "" || !cf.Success {
		score = 0
	}
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func expectedCountryFromRegion(region string) string {
	region = strings.ToLower(strings.TrimSpace(region))
	switch region {
	case "", "all", "other":
		return ""
	default:
		return strings.ToUpper(region)
	}
}

func firstError(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
