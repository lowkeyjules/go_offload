// greetings.go
package main

import (
	"encoding/base64"
	"fmt"
	"os"
)

func Greetings(name string) string {
	return "Hello, " + name + "!"
}

func main() {
	raw, _ := base64.StdEncoding.DecodeString(os.Args[1])
	result := Greetings(string(raw))
	fmt.Print(base64.StdEncoding.EncodeToString([]byte(result)))
}
