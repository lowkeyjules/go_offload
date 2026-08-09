package main

import (
	"go_offload/offload"
)

// TODO Define your Input Struct

// TODO Define your offloadable function
// add 'offload' annotation

func main() {

	url := "http://localhost:4080"

	// Offloading logic starts here
	o := offload.OpenOffload(url, 5, "main.go")
	defer o.Close()

	// TODO Put your Submit/Dispatch - Calls here
}

