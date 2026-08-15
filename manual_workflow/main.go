package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"

	"wasi.team/client"
	wasimoffv1 "wasi.team/proto/v1"
)

func main() {
	// Read Wasm binary from file system
	bytes, _ := os.ReadFile("./greetings.wasm")

	// Create sha256 encoding over bytes
	sha := sha256.Sum256(bytes)
	// Convert to string, concat "sha256:"
	shaString := "sha256:" + hex.EncodeToString(sha[:])

	// encode parameter into base64, one of many formats which can be used for transmitting over wire
	name := "Max Mustermann"
	encodedArgs := base64.StdEncoding.EncodeToString([]byte(name))

	wasimoff := client.NewWasimoffConnectRpcClient(http.DefaultClient, "http://localhost:4080")

	// required for running task, submitted to Wasimoff
	ctx := context.TODO()

	ref := shaString
	file := &wasimoffv1.File{Ref: &ref}

	request := wasimoffv1.Task_Wasip1_Request{
		Params: &wasimoffv1.Task_Wasip1_Params{
			Binary: file,
			Args:   []string{"greetings.wasm", encodedArgs},
		},
	}

	// Running task
	response, err := wasimoff.RunWasip1(ctx, &request)
	if err != nil {
		fmt.Println("request failed:", err)
		return
	}

	// handling response, checking for errors and finally decoding
	if e := response.GetError(); e != "" {
		fmt.Println("execution error:", e)
		return
	}

	out := response.GetOk()
	if out == nil {
		fmt.Println("no output from broker")
		return
	}

	if status := out.GetStatus(); status != 0 {
		fmt.Printf("exit status %d: %s\n", status, out.GetStderr())
		return
	}

	resultBytes, err := base64.StdEncoding.DecodeString(string(out.GetStdout()))
	if err != nil {
		fmt.Println("decode error:", err)
		return
	}

	fmt.Println("result:", string(resultBytes))
}
