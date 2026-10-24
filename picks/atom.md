---
title: A.T.O.M — the 5G ray tracer that refuses to lie to you
subtitle: Thirty thousand lines of Go that plan cell towers over Ankara, and print their own limitations in every JSON response. We read all of it. We're in love.
date: 2026-10-24
author: goph.re
repo: github.com/Berk-Unsal/atom
homepage: https://atom.berkunsal.com/
license: MIT
version: v0.13.0
stars: 38
tags: rf, 5g, geospatial, concurrency, gin, rtree
image: /gfx/og/picks/atom.jpg
verdict: The most honest simulation engine we've read this year.
og_kicker: #1
og_title: A.T.O.M<i>.</i>
og_sub: The 5G ray tracer that refuses to lie to you. 30k lines of Go, read by us.
og_chips: go,rf,5g,rtree,ankara
---

Let's get the disclaimer out of the way, since the project we're about to gush over would insist on one: **this review is not neutral.** Picks is where goph.re writes about Go software that makes us happy, and [A.T.O.M](https://github.com/Berk-Unsal/atom) made us very happy.

A.T.O.M, the *Ankara Telecom Optimization Model*, is an RF planning workbench. You give it cell towers on a real map of Ankara, it fires rays through 111 MB of OpenStreetMap buildings, tells you where 4G at 2.6 GHz and 5G mmWave at 28 GHz actually reach, how badly neighbouring cells interfere, and whether rotating your antennas would serve more people. There's even a 6G profile at 140 GHz, clearly labelled *research*.

That alone would be a nice Go project. What made us write this article is something rarer: **the code is relentlessly honest about what it doesn't know.** Every model checks whether it applies before it runs. Every fallback is recorded. Every optimizer result says it is a heuristic. Every explanation ships with a sentence explaining what it is *not*. In a world of dashboards that print three significant digits on a guess, that's a statement.

We read the backend at [`fc0f1e4`](https://github.com/Berk-Unsal/atom/tree/fc0f1e489110bbd30277c3454e4db95e0ca302d6) (v0.13.0). All links below are permalinks into that commit.

## At a glance

| | |
|---|---|
| Language | Go 1.26 backend, React front-end, Python data pipeline |
| Size | ~33,500 lines of Go, plus ~21,500 lines of Go tests |
| Tests | 425 `Test*`, 11 `Benchmark*` |
| HTTP | Gin, behind its own admission control |
| Spatial index | [`tidwall/rtree`](https://github.com/tidwall/rtree), generic flavour |
| Age | Created June 2026, 22 releases by v0.13.0 (22 Oct 2026) |
| License | MIT |

The dependency list is short enough to read aloud: Gin, CORS, `tidwall/rtree`, `golang.org/x/sys`. Everything else, from the propagation models and ray marching to the Pareto search and the cgroup parser, is hand-written.

## Rays, buildings and a lot of 25-metre steps

A static simulation starts in [`SimulateStaticRaysContext`](https://github.com/Berk-Unsal/atom/blob/fc0f1e489110bbd30277c3454e4db95e0ca302d6/backend-go/raytracer/static_simulation.go#L441). By default it spreads 60 rays evenly across the antenna's beam, then marches each one outward in 25 m steps. At every step, it asks the spatial index which building footprints the segment could touch, sorts the wall crossings by distance and charges each one a band-dependent penetration loss. When the received power falls below the receiver's sensitivity, it doesn't just stop at the last step: it **bisects** the segment 32 times to find the exact cutoff distance, driven by a closure over the precomputed path geometry.

The index itself is a lovely example of how small a good abstraction can be. The whole query surface is one method:

```go
func (idx *BuildingIndex) SearchBounds(bounds Bounds) []*BuildingFootprint {
	if idx == nil || !bounds.Valid() {
		return nil
	}

	candidates := make([]*BuildingFootprint, 0, 16)
	idx.tree.Search(bounds.Min(), bounds.Max(), func(_ [2]float64, _ [2]float64, footprint *BuildingFootprint) bool {
		candidates = append(candidates, footprint)
		return true
	})
	return candidates
}
```

<small>[`building_index.go#L411`](https://github.com/Berk-Unsal/atom/blob/fc0f1e489110bbd30277c3454e4db95e0ca302d6/backend-go/raytracer/building_index.go#L411-L422)</small>

`tree` is an `rtree.RTreeG[*BuildingFootprint]`, the generic R-tree, so there is no `interface{}` and no type assertion on the hot path. A ray query is literally "the bounding box of this segment". The buildings are loaded with a streaming `json.Decoder` over a 1 MiB buffered reader, with a hard 512 MiB cap and a check that the file is a regular file. That's how you ingest a city without trusting it.

## Concurrency, the boring (read: good) way

There's no framework, no `errgroup`, no clever lock-free trick, just the most legible worker pool you'll find. Workers are `min(NumCPU, 4, rays)`, jobs are ray indexes on an unbuffered channel, and the first error cancels everyone else:

```go
segments, terminal, err := simulateSegmentedRayWithBudgetContext(workCtx, origin, index, angle, req, buildings, featureBudget)
if err != nil {
	select {
	case workerErrors <- err:
	default:
	}
	cancel()
	return
}
profiles[index] = rayCoverageProfile{angle: angle, segments: segments, terminal: terminal}
```

<small>[`static_simulation.go#L1352-L1363`](https://github.com/Berk-Unsal/atom/blob/fc0f1e489110bbd30277c3454e4db95e0ca302d6/backend-go/raytracer/static_simulation.go#L1338-L1395)</small>

Look at the last line. Each worker writes into **its own pre-assigned slot** of a preallocated slice. There's no mutex, no result channel, and the output order cannot depend on the scheduler. This is the single most underrated trick for deterministic parallel code in Go, and A.T.O.M uses it exactly where it matters. The one-slot error channel with a non-blocking send keeps "first error wins" without ever blocking a worker.

Two more details we loved:

- **An output budget enforced twice.** A response is capped at 25,000 GeoJSON features. The cap is estimated up front, with an integer-overflow guard (`int(^uint(0) >> 1)`, the old-school way), and enforced again during the run with an `atomic.Int64`.
- **Comparable structs as cache keys.** The network optimizer memoises per-cell RF work in a `map[StaticSimulationRequest]networkCellContribution` ([`optimization_cell_cache.go`](https://github.com/Berk-Unsal/atom/blob/fc0f1e489110bbd30277c3454e4db95e0ca302d6/backend-go/raytracer/optimization_cell_cache.go#L20)). No hashing helper, no string key: the whole request struct *is* the key, because Go lets it be.

## Models that know when they don't apply

Here is the interface every propagation model implements, with the comment that explains the whole project's philosophy:

```go
// PropagationModel is the narrow abstraction implemented by every selectable
// model. It keeps applicability separate from numeric evaluation so an
// inapplicable urban request cannot silently become an undocumented formula.
type PropagationModel interface {
	ID() string
	Description() string
	Applicability(PropagationLinkContext) PropagationApplicability
	Evaluate(PropagationLinkContext) PropagationResult
}
```

<small>[`propagation_model.go#L126`](https://github.com/Berk-Unsal/atom/blob/fc0f1e489110bbd30277c3454e4db95e0ca302d6/backend-go/raytracer/propagation_model.go#L126-L132)</small>

The default `urban_short_range` model is the 3GPP TR 38.901 urban-macro formula, with its breakpoint distance and NLOS branch. It refuses links outside its validity range (10 m to 5 km, base station 10 to 150 m high, outdoor to outdoor). When it refuses, the engine falls back to the legacy free-space-plus-walls model **and says so** in every result: the requested `ModelID`, the `AppliedModelID`, `FallbackUsed`, and a human-readable applicability reason.

Building heights get the same treatment. A height is either `observed_tag`, `derived_from_levels`, or `unavailable`, and a fallback height is never promoted to roof evidence. A building of unknown height is treated as blocking, which is the conservative choice. Every ray segment carries its model identity, its LOS classifier, its terrain status and its full link budget. You can audit a single pixel of coverage back to the formula that produced it.

## An optimizer that admits it's a heuristic

The headline question A.T.O.M answers is: *for two to eight cells, which azimuths best serve the demand?* Five objectives (demand, residential coverage, area coverage, overlap, radio quality) are normalised against fixed domain totals, never against the candidate set, so scores stay comparable across runs. Optional constraints are applied lexicographically: a feasible plan always beats an infeasible one, then the score decides.

There are three search policies, and their names tell you everything about the author:

- `legacy_two_pass_coordinate`: two passes of coordinate descent over 36 headings per cell, at a fixed and predictable cost.
- `deterministic_multistart_coordinate_v1`: restarts from rotated baselines and memoises every state in a ledger keyed by SHA-256. It detects cycles and stops on `coordinate_stable`, `max_passes`, `repeated_state` or `evaluation_budget`. Its result metadata literally says `Heuristic: true, GloballyOptimal: false, SearchGuarantee: "heuristic_local_search"`.
- `deterministic_pareto_archive_search_v1`: grows an archive of non-dominated plans. The best comment in the codebase lives here:

```go
// Keep the production Pareto contract: every objective that exists in the
// fixed domain participates when available. User weights are ranking inputs,
// and omission/zero weight must not silently remove a discovery dimension.
```

<small>[`optimization_pareto_archive.go#L127`](https://github.com/Berk-Unsal/atom/blob/fc0f1e489110bbd30277c3454e4db95e0ca302d6/backend-go/raytracer/optimization_pareto_archive.go#L126-L140)</small>

Read that twice. Weights only rerank the frontier, they never decide what gets *discovered*. Setting a priority to zero can't hide a better trade-off from you. That's a product decision expressed as three lines of Go comment, and the code below follows it.

There is no `math/rand` anywhere in the engine. Every sort is a `sort.SliceStable` with an explicit tie-break (equal score falls back to tower key, then admission order, then state key). Azimuths are rounded to 1e-6 before being hashed. Determinism is a feature here, and there are tests named `...IsDeterministic` to prove it.

## Explanations that come with their own disclaimer

Ask A.T.O.M why it rotated a given cell, and it does something honest: it re-evaluates the chosen plan with *only that cell* reverted to its original azimuth, and shows you both sides, the metric deltas and the score. Then it adds this to the response:

```go
Limitations: []string{
	"Conditional marginal comparison: selected configuration versus the same network with this cell reverted to baseline.",
	"This is not causal attribution, an independent cell contribution, or an additive decomposition; cell interactions remain.",
},
```

<small>[`optimization_explanation.go#L309`](https://github.com/Berk-Unsal/atom/blob/fc0f1e489110bbd30277c3454e4db95e0ca302d6/backend-go/raytracer/optimization_explanation.go#L309-L312)</small>

We have reviewed ML products with bigger budgets that never wrote that sentence.

## Validation that refuses to overclaim

Feed it real RSRP drive-test samples and A.T.O.M computes residuals, MAE, RMSE, bias and P50/P90 errors. It will even suggest a bias correction, but only after clearing a gauntlet of checks:

- at least 20 samples, spread over at least five distinct 50 m areas spanning 100 m or more;
- a spatially blocked 5-fold cross-validation (no random row splits leaking neighbours into the test set);
- a confidence interval on the median;
- and it only suggests the correction if it actually lowers the error.

The canonical validation hard-codes `ProductionCandidate: false` and `CalibrationActive: false`, never pools frequencies or LOS/NLOS groups, and when you give it nothing to validate against, it tells you so:

```go
response.Notes = append(response.Notes, "No measurement observations were supplied; no real-data validation result is claimed.")
```

The README is just as blunt: *"Measured real-world propagation accuracy is not yet established."* We'd frame that sentence.

## Production hygiene from a one-person project

Expensive RF endpoints sit behind two layers of admission control. Each client gets a budget (20 requests a minute, one in flight), tracked for at most 4,096 clients with idle eviction. Denial logs carry a `maphash` of the client key, never the raw address. Then a global semaphore that never queues, because a planning engine should say "busy" rather than pile up work:

```go
select {
case limiter.slots <- struct{}{}:
	defer func() {
		<-limiter.slots
		release()
	}()
	c.Next()
default:
	release()
	limiter.logDenial(c, resource, clientID, requestID, remaining, 1, "capacity_busy")
	c.Header("Cache-Control", "no-store")
	c.Header("Retry-After", "1")
	c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": resource + " capacity is busy; retry shortly"})
}
```

<small>[`rf_protection.go#L130`](https://github.com/Berk-Unsal/atom/blob/fc0f1e489110bbd30277c3454e4db95e0ca302d6/backend-go/rf_protection.go#L130-L143)</small>

A buffered channel as a semaphore, a `select` with `default` for "try-acquire", and `Retry-After` plus `RateLimit-*` headers so well-behaved clients can back off. It's textbook, and it's exactly right.

Around it:

- **Strict request parsing.** Bodies are capped at 1 MiB, and JSON is decoded with `DisallowUnknownFields` and rejected if anything trails the first value.
- **Real server timeouts.** The `http.Server` sets explicit timeouts, and SIGTERM triggers a 30-second graceful shutdown.
- **API key compared safely.** The optional API key is checked by hashing both sides and comparing in constant time.
- **A cgroup reader that tells you how it knows.** The resource profile joins `/proc/self/cgroup` with `mountinfo` instead of assuming `/sys/fs/cgroup`, and handles cgroup v1, v2 and hybrid. Every value is a generic `Signal[T]` that records *how* it knows: known, unlimited, absent or unknown.
- **A pinned supply chain.**
  - Every Docker `FROM` is pinned by digest, the final image runs as a non-root user, and it has a `/readyz` health check.
  - Every GitHub Action is pinned to a full commit SHA, and scripts in CI enforce both rules.
  - `govulncheck`, hash-pinned Python requirements and dependency review run on every change.
- **Rules for contributors, humans or AI.** An `AGENTS.md` asks them to run `go test -race`, keep the RF math deterministic, add a regression test with every fix and write Conventional Commits.

## Where we'd send a PR

We promised to be biased, not blind. Reading 33k lines, here's what we'd fix first, roughly by impact.

**Swallowed errors in the single-cell sweep.** In `OptimizeAzimuthContext`, a worker that hits an error simply returns:

```go
breakdown, err := CoverageAreaScoreBreakdownContext(ctx, origin, testReq, buildings)
if err != nil {
	return
}
```

<small>[`static_simulation.go#L555-L558`](https://github.com/Berk-Unsal/atom/blob/fc0f1e489110bbd30277c3454e4db95e0ca302d6/backend-go/raytracer/static_simulation.go#L526-L582)</small>

The error is lost and that heading keeps a zero-value result. If all four workers fail, the producer blocks on the unbuffered `jobs` channel until the request deadline fires (60 s by default) instead of failing fast. The fix is already written, about 800 lines further down: it's the exact error-channel-plus-`cancel()` pattern from the ray pool. The same pool is copy-pasted three times (rays, sweep, interference), which is how one copy drifted. An `errgroup` with `SetLimit(4)` would make all three shorter and correct.

**Determinism has a hole, and it's a map.** Demand and residential scores are summed by ranging over Go maps ([`static_simulation.go#L1557`](https://github.com/Berk-Unsal/atom/blob/fc0f1e489110bbd30277c3454e4db95e0ca302d6/backend-go/raytracer/static_simulation.go#L1557-L1568)). Go randomises map iteration order, and floating-point addition isn't associative, so two identical runs can disagree in the last bits. That's harmless for display, but this is an optimizer comparing scores with `>`, in a project that prides itself on determinism. Sorting the keys before summing would close it.

**A tie-break that isn't one.** When picking the building at a ray endpoint:

```go
if selected == nil || building.HeightMeters > selected.HeightMeters || building.ID < selected.ID {
```

<small>[`height_obstruction.go#L327`](https://github.com/Berk-Unsal/atom/blob/fc0f1e489110bbd30277c3454e4db95e0ca302d6/backend-go/raytracer/height_obstruction.go#L327)</small>

A shorter building with a smaller ID replaces a taller one, so the result depends on R-tree iteration order. It should probably be `> || (== && <)`.

**A sector antenna twice as narrow as advertised (we think).** The cosine-sector pattern passes `BeamWidthDeg/2` into a helper whose parameter is named `threeDBWidthDeg` and implements the 3GPP `12·(θ/θ3dB)²` law, where θ3dB is the *full* 3 dB width. A nominal 120° beam is therefore 12 dB down at ±60°, not 3 dB. It may be a deliberate modelling choice, but the parameter name says otherwise.

**Smaller things:**

- `AvgRxDBm` is the arithmetic mean of dBm values rather than of power.
- dBm, dB and mW are all plain `float64` distinguished by naming, so a few `type DBm float64` would catch unit mistakes at compile time.
- `static_simulation.go` is 2,239 lines, and has a chain of five thin `simulateSegmentedRay…` wrappers around one core function that often discard `err` with `_`.
- There are no fuzz tests, and the hand-written cgroup and mountinfo parsers are the perfect first targets for `go test -fuzz`.
- Async experiment workers run on `context.Background()`, so graceful shutdown doesn't cancel them, and they bypass the global RF semaphore.
- A 1.2 MB `286.csv` at the repo root looks like an OpenCellID extract for Turkey. That dataset is CC BY-SA, so it deserves an attribution note next to the MIT license.

None of this dents our enthusiasm. Every item is a small, well-scoped PR, and the codebase is clean enough that you can find each one in minutes. That itself is a compliment.

<div class="verdict-box">
<div class="love">
<h3>Why we love it</h3>
<ul>
<li>Honesty as an architecture: applicability checks, recorded fallbacks, provenance on every segment, disclaimers in every explanation.</li>
<li>Deterministic parallelism done the simple way: slot-per-index results, stable sorts, explicit tie-breaks, no randomness.</li>
<li>A Pareto search that refuses to let weights hide discoveries.</li>
<li>Admission control, request hygiene and supply-chain discipline you'd expect from a team, not a solo project.</li>
<li>Small dependency surface: Gin, an R-tree, and a lot of careful Go.</li>
</ul>
</div>
<div class="meh">
<h3>Where it could be better</h3>
<ul>
<li>Three copies of the worker pool, one of which swallows errors.</li>
<li>Map-order float sums undermine the determinism story.</li>
<li>No unit types for dB/dBm/mW, very long files, wrapper chains.</li>
<li>No fuzzing on hand-written parsers.</li>
</ul>
</div>
</div>

## Try it

You'll need Docker and Git LFS (the Ankara buildings are 111 MB):

```bash
git lfs install
git clone https://github.com/Berk-Unsal/atom.git
cd atom
git lfs pull
docker compose up --build -d atom
# then open http://localhost:8080
```

Pick **Open sample network project** on the first-run card, select two cells and hit **Evaluate Network**. Then open the JSON in your browser's network tab and read the `limitations`, `applicability` and `notes` fields. That's where this project really shines.

Or skip the install and play with the [live demo](https://atom.berkunsal.com/).

If you're into geospatial Go, RF, or simply want to see what *careful* looks like in a fast-moving solo project, go star [Berk-Unsal/atom](https://github.com/Berk-Unsal/atom). And Berk, if you're reading this: we'll happily send the `errgroup` PR.
