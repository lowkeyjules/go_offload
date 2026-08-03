package main

import (
	"fmt"
	"go_offload/offload"
	"math"
	"os"
	"strings"
	"time"
)

type GreetingParams struct {
	Name string
}

// offload
func Greeting(in GreetingParams) string {
	return "Hello, " + in.Name + "!"
}

type MatMulParams struct {
	A [][]float64
	B [][]float64
}

// offload
func MatrixMultiplication(in MatMulParams) [][]float64 {
	n := len(in.A)
	out := make([][]float64, n)
	for i := range out {
		out[i] = make([]float64, n)
		for j := 0; j < n; j++ {
			sum := 0.0
			for k := 0; k < n; k++ {
				sum += in.A[i][k] * in.B[k][j]
			}
			out[i][j] = sum
		}
	}
	return out
}

type TimingReturn struct {
	Sum      float64
	Started  time.Time
	Duration time.Duration
}

type WorkloadParams struct {
	N int
}

// offload
func Benchmark(in WorkloadParams) TimingReturn {
	start := time.Now()

	sum := 0.0
	for i := 1; i <= in.N; i++ {
		sum += math.Sqrt(float64(i))
	}

	return TimingReturn{
		Sum:      sum,
		Started:  start,
		Duration: time.Since(start),
	}
}

func main() {

	if len(os.Args) < 2 {
		fmt.Println("Usage: go run demo.go <BROKER URL>")
		os.Exit(1)
	}

	url := os.Args[1]
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "http://" + url
	}

	o := offload.OpenOffload(url, 10, "demo.go")
	defer o.Close()

	// Submit - Offloading Examples:

	result, err := offload.Submit[string](o, "Greeting", GreetingParams{Name: "Player 1"})
	if err != nil {
		fmt.Println(err)
	}
	fmt.Printf("Hello result from Submit: %v\n", result)

	result2, err2 := offload.Submit[TimingReturn](o, "Benchmark", WorkloadParams{1_000_000_000})
	if err2 != nil {
		fmt.Println(err2)
	}
	fmt.Printf("Benchmark result from Submit: %v\n", result2.Duration)

	result3, err3 := offload.Submit[[][]float64](o, "MatrixMultiplication", MatMulParams{
		A: [][]float64{{1, 2}, {3, 4}},
		B: [][]float64{{5, 6}, {7, 8}},
	})
	if err3 != nil {
		fmt.Println(err3)
	}
	fmt.Printf("MatrixMultiplication result from Submit: %v\n", result3)

	// SubmitAll - Offloading Example:

	resultAll, errorAll := offload.SubmitAll[TimingReturn](o, "Benchmark", WorkloadParams{200_000_000}, WorkloadParams{100_000_000}, WorkloadParams{1_000_000})

	for _, err := range errorAll {
		if err != nil {
			fmt.Println(errorAll)
		}
	}

	for _, r := range resultAll {
		fmt.Printf("Result from SubmitAll: %v\n", r.Duration)
	}

	// Dispatch - Offloading Example:

	resCh, errCh := offload.Dispatch[[][]float64](o, "MatrixMultiplication", MatMulParams{
		A: [][]float64{{1, 2}, {3, 4}},
		B: [][]float64{{5, 6}, {7, 8}},
	})

	errorDispatch := <-errCh
	resultDispatch := <-resCh

	if errorDispatch != nil {
		fmt.Println(errCh)
	}

	fmt.Printf("Result from Dispatch: %v\n", resultDispatch)

	// DispatchAll - Offloading Example:

	resAllCh, errAllCh := offload.DispatchAll[TimingReturn](o, "Benchmark",
		WorkloadParams{200_000_000},
		WorkloadParams{100_000_000},
		WorkloadParams{1_000_000},
	)

	errorDispatchAll := <-errAllCh
	resultsDispatchAll := <-resAllCh

	for _, err := range errorDispatchAll {
		if err != nil {
			fmt.Println(err)
		}
	}

	for _, r := range resultsDispatchAll {
		fmt.Printf("Result from DispatchAll: %v\n", r.Duration)
	}

}
