package main

import (
	"encoding/csv"
	"fmt"
	"go_offload/offload"
	"log"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// --------------TSP---------------

type TSPResult struct {
	Order  []int
	Length float64
}

type TSPParam struct {
	Dist [][]float64
}

// local func
func randomMatrix(n int, seed int64) [][]float64 {
	r := rand.New(rand.NewSource(seed))
	type pt struct{ x, y float64 }
	pts := make([]pt, n)
	for i := range pts {
		pts[i] = pt{r.Float64() * 100, r.Float64() * 100}
	}
	dist := make([][]float64, n)
	for i := range dist {
		dist[i] = make([]float64, n)
		for j := range dist[i] {
			dx := pts[i].x - pts[j].x
			dy := pts[i].y - pts[j].y
			dist[i][j] = math.Sqrt(dx*dx + dy*dy)
		}
	}
	return dist
}

// offload
func Tsp(dist TSPParam) TSPResult {
	n := len(dist.Dist)
	if n <= 1 {
		order := make([]int, n)
		for i := range order {
			order[i] = i
		}
		return TSPResult{Order: order, Length: 0}
	}

	// Tourlänge: Start und Ende fix bei Stadt 0
	tourLength := func(perm []int) float64 {
		total := dist.Dist[0][perm[0]]
		for i := 0; i+1 < len(perm); i++ {
			total += dist.Dist[perm[i]][perm[i+1]]
		}
		total += dist.Dist[perm[len(perm)-1]][0]
		return total
	}

	// permutiere Städte 1..n-1, Stadt 0 bleibt Startpunkt
	m := n - 1
	perm := make([]int, m)
	for i := range perm {
		perm[i] = i + 1
	}

	bestOrder := make([]int, m)
	copy(bestOrder, perm)
	bestLen := tourLength(perm)

	// Heap's Algorithmus (iterativ) -> alle (n-1)! Permutationen
	c := make([]int, m)
	i := 0
	for i < m {
		if c[i] < i {
			if i%2 == 0 {
				perm[0], perm[i] = perm[i], perm[0]
			} else {
				perm[c[i]], perm[i] = perm[i], perm[c[i]]
			}
			if l := tourLength(perm); l < bestLen {
				bestLen = l
				copy(bestOrder, perm)
			}
			c[i]++
			i = 0
		} else {
			c[i] = 0
			i++
		}
	}

	full := make([]int, 0, n)
	full = append(full, 0)
	full = append(full, bestOrder...)
	return TSPResult{Order: full, Length: bestLen}
}

// ----------Benchmark harness----------

// sinkTSP stops the compiler from optimising the local loop away.
var sinkTSP TSPResult

// Row is one measurement written to the CSV.
type Row struct {
	Method      string
	BatchSize   int
	ProblemSize int
	Iteration   int
	DurationMs  float64
}

func ms(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000.0
}

// ---- one runner per method, each times a whole batch of "batch" tasks ----

// Submit: blocking, one task at a time -> fully sequential.
func runSubmit(o *offload.Orchestrator, payloads []any) time.Duration {
	start := time.Now()
	for _, p := range payloads {
		offload.Submit[TSPResult](o, "Tsp", p)
	}
	return time.Since(start)
}

// SubmitAll: blocking, but hands the whole batch over at once, so the
// tasks run concurrently on the broker.
func runSubmitAll(o *offload.Orchestrator, payloads []any) time.Duration {
	start := time.Now()
	offload.SubmitAll[TSPResult](o, "Tsp", payloads...)
	return time.Since(start)
}

// Dispatch: async single. Fire every task first (each returns a channel),
// then wait for all results -> also concurrent.
func runDispatch(o *offload.Orchestrator, payloads []any) time.Duration {
	start := time.Now()
	chans := make([]chan TSPResult, len(payloads))
	for i, p := range payloads {
		chans[i] = offload.Dispatch[TSPResult](o, "Tsp", p)
	}
	for _, ch := range chans {
		<-ch
	}
	return time.Since(start)
}

func runDispatchAll(o *offload.Orchestrator, payloads []any) time.Duration {
	start := time.Now()
	ch := offload.DispatchAll[TSPResult](o, "Tsp", payloads...)
	<-ch
	return time.Since(start)
}

func runLocal(payloads []any) time.Duration {
	start := time.Now()
	for _, p := range payloads {
		sinkTSP = Tsp(p.(TSPParam))
	}
	return time.Since(start)
}

func main() {
	o := offload.OpenOffload("http://localhost:4080", 12000, "./benchmark/benchmark.go")
	defer o.Close()

	const problemSize = 11
	const iters = 20
	batchSizes := []int{1, 2, 4, 8, 16}

	param := TSPParam{Dist: randomMatrix(problemSize, 30)}

	offload.Submit[TSPResult](o, "Tsp", param)

	var rows []Row
	for _, batch := range batchSizes {
		// build the payload slice once per batch size (all identical)
		payloads := make([]any, batch)
		for i := range payloads {
			payloads[i] = param
		}

		for it := 0; it < iters; it++ {
			rows = append(rows, Row{"Submit", batch, problemSize, it, ms(runSubmit(o, payloads))})
			rows = append(rows, Row{"SubmitAll", batch, problemSize, it, ms(runSubmitAll(o, payloads))})
			rows = append(rows, Row{"Dispatch", batch, problemSize, it, ms(runDispatch(o, payloads))})
			rows = append(rows, Row{"DispatchAll", batch, problemSize, it, ms(runDispatchAll(o, payloads))})
			rows = append(rows, Row{"Local", batch, problemSize, it, ms(runLocal(payloads))})
		}
		fmt.Printf("done batch size %d\n", batch)
	}

	writeCSV("./benchmark/results/bench_results.csv", rows)
	fmt.Println("wrote ./benchmark/results/bench_results.csv")
}

func writeCSV(filename string, rows []Row) {
	if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
		log.Fatal(err)
	}
	f, err := os.Create(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	w.Write([]string{"method", "batch_size", "problem_size", "iteration", "duration_ms"})
	for _, r := range rows {
		w.Write([]string{
			r.Method,
			strconv.Itoa(r.BatchSize),
			strconv.Itoa(r.ProblemSize),
			strconv.Itoa(r.Iteration),
			strconv.FormatFloat(r.DurationMs, 'f', 7, 64),
		})
	}
}
