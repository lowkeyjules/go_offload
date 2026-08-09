package main

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"math"
	"os"
	"time"
)

type GreetingParams struct {
	Name string
}

type MatMulParams struct {
	A [][]float64
	B [][]float64
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

type Return struct {
	ReturnEncoded string
	ErrMsg        string
}

// encodes and returns Return{ErrMsg: msg} !
func fail(msg string) {
	wrapped := Return{ErrMsg: msg}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(wrapped); err != nil {
		// last resort
		os.Exit(1)
	}

	fmt.Println(base64.StdEncoding.EncodeToString(buf.Bytes()))
	os.Exit(0)
}

func main() {
	if len(os.Args) < 2 {
		fail("missing input")
	}

	raw, err := base64.StdEncoding.DecodeString(os.Args[1])
	if err != nil {
		fail("base64 decode error: " + err.Error())
	}

	var in WorkloadParams
	if err := gob.NewDecoder(bytes.NewReader(raw)).Decode(&in); err != nil {
		fail("gob decode error: " + err.Error())
	}

	result := Benchmark(in)

	var resBuf bytes.Buffer
	if err := gob.NewEncoder(&resBuf).Encode(result); err != nil {
		fail("gob encode error (result): " + err.Error())
	}

	wrapped := Return{ReturnEncoded: base64.StdEncoding.EncodeToString(resBuf.Bytes())}

	var outBuf bytes.Buffer
	if err := gob.NewEncoder(&outBuf).Encode(wrapped); err != nil {
		fail("gob encode error (wrapper): " + err.Error())
	}

	os.Stdout.Write(outBuf.Bytes())
}
