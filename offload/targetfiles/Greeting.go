package main

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"fmt"
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
func Greeting(in GreetingParams) string {
	return "Hello, " + in.Name + "!"
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

	var in GreetingParams
	if err := gob.NewDecoder(bytes.NewReader(raw)).Decode(&in); err != nil {
		fail("gob decode error: " + err.Error())
	}

	result := Greeting(in)

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
