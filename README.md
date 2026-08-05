# Go Offload
___

**Computation offloading** is the act of moving computation away from local execution onto less constrained devices.
This includes cloud servers, edge devices as well as hardware accelerators like GPUs and FPGAs.
There are many different reasons for offloading, although most cases involve offloading from mobile devices like phones, 
due to technical limitations and battery constraints.

**Go Offload** is a research prototype developed to accelerate the workflow for offloading WebAssembly (Wasm) modules.
It is designed to work together with the framework [Wasimoff](https://github.com/wasimoff/wasimoff), which is used as the computational backend.
The core idea of this tool is to use annotations to reduce the amount of work a developer has to  do before offloading a module.

By using the annotation ``// offload`` in front of a function within the parsed file, a WebAssembly module is built. 
Just with one annotation and API-Call these modules can be used for remote offloading.

The following steps are handled by Go Offload:
- Writing and Generating Wasm modules
- Connecting to remote offloading framework
- Upload and submission of tasks
- Handling in- and output to and from backend
- Offering concurrency and non-blocking calls

The above listed functions are all handled at runtime.

This tool is currently still under development and might undergo some changes in the future.

> [NOTE]
> This project is part of an ongoing research study. If you try Go Offload, I'd appreciate it if you filled out [this survey](https://forms.gle/QdQvoCPe7SATBibY8) (~5-10 minutes, anonymous) afterward.
> Some parts of the survey also require you to have read the [Manual Workflow: Offloading without annotations](#manual-workflow-offloading-without-annotations) section.


## How it works
___
The process can be split into two parts. 
In the first half the WebAssembly modules are built and uploaded to the backend, after which the actual offloaded task execution proceeds.

By initializing the Offloader and passing the target file for parsing, the parser scans the file for ``// offload``
annotations, creates the same amount of Wasm files as there are annotated functions and uploads them to Wasimoff's broker.
The program checks whether these modules are already uploaded by comparing their SHA256 encodings to those in the remote storage.
If they do not exist yet, they are uploaded. If they do, the program proceeds.
```go
// offload
func add(in InputStruct) int {
	return in.First + in.Second
}
```

All modules are based on the same code skeleton. Used imports as well as all kinds of struct declarations are copied into the Wasm modules to avoid 
runtime errors when run remotely.
Compilation target is WASIp1.

When either dispatching (non-blocking) or submitting (blocking), the program checks whether the submitted task was already uploaded to the broker.
If not, the program terminates on an error.
The input strictly accepts one struct. This is a deliberate design choice that allows Go specific types by using Gob Encoding, although burdening the
developer in using structs when defining the functions.
The return type is encoded by Gob as well, though it can be decoded within the dispatch/submit call. 
For error handling and blocking/non-blocking functionality, the following return types have been implemented:

| Function    | Return Type              |
|-------------|--------------------------|
| Submit      | (T, error)               |
| SubmitAll   | ([]T, []error)           |
| Dispatch    | (chan T, chan error)     |
| DispatchAll | (chan []T, chan []error) |

T is referring to [T any] which is a declaration for T as the 'any' type. 
The 'any' type is used in Go generics, where it acts as a constraint for primitive datatypes as well as structs, maps and other Go-specific types. 

The return does not have the same restrictions as the input type and can be any type.
The Submit/Dispatch calls use the case-sensitive name of the function to load their SHA-encodings and reference them on the uploading framework.
Parameters are transmitted as one Gob-serialized, base64-encoded string that is passed as an argument into the Wasm binary, which is 
being decoded within the Wasm module and injected as the input into the function.

## Usage
___

### Requirements
Check if the following requirements are fulfilled before executing the program:
- Docker is installed
- Go is installed

For Docker, use the following guide for Docker installation: [Docker Desktop](https://docs.docker.com/desktop/).

For Go, follow the [Go Installation Guide](https://go.dev/doc/install) and install the latest versions for your system.

### Setting up the environment


Start up the container by executing the following lines of code in your terminal in project root:
```bash 
docker compose pull
docker compose up broker provider
```
Now wait for the container to start. 

Execute program by typing the following in your terminal:
```bash
go run main.go
```

### Using the Go Offloader
Now you can use the demo.go as an existing example and experiment in the provided environment, the main.go file.

Before the main-function, define the functions you want to offload and add the ``// offload`` annotation.
Define the input structs, as well as all other structs that might get used within your function. 
It might look something like this:

````go
import "go_offload/offload"

// Use exported types for the input Struct so that it can be transmitted on the wire
type GreetParam struct {  
    Name string  
}

// The Input is limited to exactly one struct with arbitrary field types
// The return also expects exactly one parameter, can be any go-specific or non-specific type

// offload    
func Greetings(g GreetParam) string{
    return "Hello, " + g.Name + "!"
}


func main(){
	    // Initialization of the Offloader as well as the 
	    // Submit/Dispatch Calls go here.
	    // Finally, don't forget to call Close()
	}
````

Note that all fields of a struct should be declared in uppercase, so that these types are exported by Gob.

Create the offloader within the main-function with the following lines: 
````go
// url is the API point to connect to Wasimoff's broker
// timeout is in seconds, if timeout is below 5, set to 5
// file to parse, main.go is used for this example
o := OpenOffload(url : url, timeout: 5, file: "main.go")
defer o.Close()
````
The submit/dispatch functions are generic functions with a type parameter T, which either returns that type or a channel of that type.
The following parameters take the Offloader object first, as all offloadable messages are pushed into a channel here.
Followed by the input structs. SubmitAll() and DispatchAll() take multiple structs as arguments and handle them concurrently.

Following our examples, the calls can look like this: 

````go
// Blocking calls, return decoded result, if result type is passed correctly
// SubmitAll returns array containing all submitted args, starts multiple routines per arg, runs concurrently
r, err := offload.Submit[string](o, "Greetings", Greeting{Name: "John Titor"})
r, errArr := offload.SubmitAll[string](o, "Greetings", Greeting{Name: "Mustermann"}, Greeting{Name: "Jules"}, Greeting{Name: "User"})

// Non-blocking calls, returns channel
// DispatchAll returns channel containing all submitted args in an array, starts multiple routines per arg, runs concurrently
ch, errCh := offload.Dispatch[string](o, "Greetings", Greeting{Name: "John Titor"})
ch, errChArr := offload.DispatchAll[string](o, "Greetings", Greeting{Name: "Mustermann"}, Greeting{Name: "Jules"}, Greeting{Name: "User"})
````
Have fun trying out Go Offload!

## Manual Workflow: Offloading without annotations
___
This paragraph provides an example of how the offloading would look like without using annotation-driven tools.
The following illustrates a minimal example of how the usual offloading pipeline looks like.

### 1. Writing the Offloadable Module
The target file will be compiled to WASIp1. This format can receive input and produce output through stdin and stdout.
This means that encoding and decoding needs to be handles through the offloadable module in addition to the actual computation logic.
Input is read throught os.Args.

The following code is a minimal example for receiving arguments, decoding them and putting them as a parameter into a function, back to encoding them into base64.

```go
// greeting.go
package main

import (
	"encoding/base64"
	"fmt"
	"os"
)

func Greeting(name string) string {
	return "Hello, " + name + "!"
}

func main() {
	raw, _ := base64.StdEncoding.DecodeString(os.Args[1])
	result := Greeting(string(raw))
	fmt.Print(base64.StdEncoding.EncodeToString([]byte(result)))
}
```

### 2. Compiling to WASIp1
The ordinary Go program still needs to be compiled to wasip1/wasm target, so it can be run by Wasimoff.
```bash
GOOS=wasip1 GOARCH=wasm go build -o greeting.wasm greeting.go
```
The result is a greeting.wasm binary is now an artifact that gets uploaded to the broker and gets distributed to different provider.

### 3. Uploading Wasm Binary to Broker
Though there is an API-endpoint for uploading the file to Wasimoff directly in Go, the following example uploads through curl.
The file can be referenced by its SHA256 on the broker later.
```bash
BROKER="http://localhost:4080"

curl -X POST -H "content-type: application/wasm" "$BROKER/api/storage/upload?name=greeting.wasm" --data-binary "@greeting.wasm"
```

### 4. Submitting the Task
As the file is now on the broker, the client can now submit tasks using the Wasimoff RPC client, rather than using the Submit/Dispatch wrapper.
```go
// main.go
package main

import (
	"context"
	"fmt"
	"net/http"

	"wasi.team/client"
	wasimoffv1 "wasi.team/proto/v1"
)

func main() {
	wasimoff := client.NewWasimoffConnectRpcClient(http.DefaultClient, "http://localhost:4080")

	ctx := context.TODO()

	ref := "sha256:<hash-of-greeting.wasm>"
	file := &wasimoffv1.File{Ref: &ref}

	request := wasimoffv1.Task_Wasip1_Request{
		Params: &wasimoffv1.Task_Wasip1_Params{
			Binary: file,
			Args:   []string{"greeting.wasm", "<base64-encoded-args>"},
		},
	}

	response, err := wasimoff.RunWasip1(ctx, &request)
	if err != nil {
		fmt.Println("request failed:", err)
		return
	}
	// see step 5 below
	_ = response
}
```
Note that the SHA256 needs to be constructed by hand.

### 5. Handling the Result
Finally, the response needs to be unwrapped, checked for RPC errors, checked for the WASIp1 exit status and decoded.
```go
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
```

All of these 5 steps are prone to error. Reducing them to a simple `// offload` annotation and few simple API-calls can significantly reduce the effort required to offload computation.
