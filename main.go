package main

import (
	"go_offload/offload"
)

// Define your Input Struct

// Define your offloadable function

func main() {

	url := "http://localhost:4080"

	// Offloading logic starts here

	o := offload.OpenOffload(url, 5, "main.go")
	defer o.Close()

	// Put your Submit/Dispatch - Calls here

}

// TODO copy multiple functions into wrapper file
