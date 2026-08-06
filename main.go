package main

import (
	"go_offload/offload"
)

// TODO Define your Input Struct here

// TODO Define your Offloadable Function here

func main() {

	url := "http://localhost:4080"

	// Offloading logic starts here
	o := offload.OpenOffload(url, 5, "main.go")
	defer o.Close()

	// TODO Put your Submit/Dispatch-Calls here

}

