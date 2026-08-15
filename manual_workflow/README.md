## Manual Workflow - Example
___
While the workflow is described in detail within the README of the project root, this file contains a short step-by-step guide for the manual execution of the contents of this folder.
You can run everything line-by-line in your shell. Make sure you **run the commands from this directory**, not the project root.

### Before execution
If Docker and Go are not installed and Docker is not running, read "Usage" > "Requirements" and "Setting up the environment" in the project root README.

### 1. Compiling greetings.go
Compiles the `greetings.go` file into a `greetings.wasm` file. Make sure it appears in this directory.
You can check with `ls` (macOS/Linux) or `dir` (Windows).

**MacOS and Linux:**
```bash
GOOS=wasip1 GOARCH=wasm go build -o greetings.wasm greetings.go
```
**Windows:**
```cmd
set GOOS=wasip1 && set GOARCH=wasm && go build -o greetings.wasm greetings.go
```

### 2. Offloading greetings.wasm
Before execution, make sure that Wasimoff is up and running. 

`main.go` contains a fully functioning program that encodes the input, uploads the Wasm binary, submits the task and decodes the return.
```cmd
go run main.go
```

You should see output similar to:
```cmd
result: Hello, Max Mustermann!
```