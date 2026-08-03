package main

import (
	"ba_jw/implementation/offload"
	"encoding/csv"
	"fmt"
	"log"
	"math"
	"os"
	"strconv"
	"time"
)

type Work struct {
	N int
}

// offload
func Compute(in Work) float64 {
	sum := 0.0
	for i := 1; i <= in.N; i++ {
		sum += math.Sqrt(float64(i))
	}
	return sum
}

var sink float64

type Row struct {
	Method     string
	BatchSize  int
	Iteration  int
	DurationMs float64
}

func ms(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000.0
}

func runSubmit(o *offload.Orchestrator, payloads []any) time.Duration {
	start := time.Now()
	for _, p := range payloads {
		offload.Submit[float64](o, "Compute", p)
	}
	return time.Since(start)
}

func runSubmitAll(o *offload.Orchestrator, payloads []any) time.Duration {
	start := time.Now()
	offload.SubmitAll[float64](o, "Compute", payloads...)
	return time.Since(start)
}

func runDispatch(o *offload.Orchestrator, payloads []any) time.Duration {
	start := time.Now()
	chans := make([]chan float64, len(payloads))
	for i, p := range payloads {
		chans[i] = offload.Dispatch[float64](o, "Compute", p)
	}
	for _, ch := range chans {
		<-ch
	}
	return time.Since(start)
}

func runDispatchAll(o *offload.Orchestrator, payloads []any) time.Duration {
	start := time.Now()
	ch := offload.DispatchAll[float64](o, "Compute", payloads...)
	<-ch
	return time.Since(start)
}

func runLocal(payloads []any) time.Duration {
	start := time.Now()
	for _, p := range payloads {
		sink = Compute(p.(Work))
	}
	return time.Since(start)
}

func main() {
	o := offload.OpenOffload("http://localhost:4080", 600, "./benchmark/api_bench.go")
	defer o.Close()

	batchSizes := []int{1, 2, 4, 8, 16}
	const iters = 20
	const workN = 2_000_000

	offload.Submit[float64](o, "Compute", Work{N: workN})

	var rows []Row
	for _, batch := range batchSizes {
		payloads := make([]any, batch)
		for i := range payloads {
			payloads[i] = Work{N: workN}
		}

		for it := 0; it < iters; it++ {
			rows = append(rows, Row{"Submit", batch, it, ms(runSubmit(o, payloads))})
			rows = append(rows, Row{"SubmitAll", batch, it, ms(runSubmitAll(o, payloads))})
			rows = append(rows, Row{"Dispatch", batch, it, ms(runDispatch(o, payloads))})
			rows = append(rows, Row{"DispatchAll", batch, it, ms(runDispatchAll(o, payloads))})
			rows = append(rows, Row{"Local", batch, it, ms(runLocal(payloads))})
		}
		fmt.Printf("done batch size %d\n", batch)
	}

	writeCSVAPI("./benchmark/results/api_bench.csv", rows)
	fmt.Println("wrote /benchmark/results/api_bench.csv")
}

func writeCSVAPI(filename string, rows []Row) {
	f, err := os.Create(filename)
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	w := csv.NewWriter(f)
	defer w.Flush()

	w.Write([]string{"method", "batch_size", "iteration", "duration_ms"})
	for _, r := range rows {
		w.Write([]string{
			r.Method,
			strconv.Itoa(r.BatchSize),
			strconv.Itoa(r.Iteration),
			strconv.FormatFloat(r.DurationMs, 'f', 3, 64),
		})
	}
}
