package offload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Orchestrator struct {
	// User config parameter
	url     string
	file    string
	timeout time.Duration

	// Objects used by Orchestrator
	parser *fileParser
	param  *parametrizer

	// Offloading helper
	offloadables chan message
	// name to sha mapping
	refs map[string]string
	// name to is-in-backend-storage mapping
	refsRemote map[string]bool
	backend    Backend
	wg         sync.WaitGroup
}

// OpenOffload is the core of the offloading program.
// It creates an orchestrator object which builds the Wasm modules
// as well as offloads functions when called.
// While the naming can be confusing, it has been deliberately chosen, to signal the user that
// the channel, which has been opened for offloading, must be closed.
// Implementation currently uses Wasimoff as computational backend.
func OpenOffload(url string, timeout int, file string) *Orchestrator {
	return OpenOffloadBackend(newBackendWasimoff(url), timeout, file)
}

// OpenOffloadBackend loosely couples backend to offloading process, to simplify adding new backends.
func OpenOffloadBackend(b Backend, timeout int, file string) *Orchestrator {
	// defaults to main.go
	if file == "" {
		file = "main.go"
	}
	if !strings.HasSuffix(file, ".go") {
		file += ".go"
	}

	if timeout < 2 {
		log.Printf("Timeout falsely set to %d, correcting to 2", timeout)
		timeout = 2
	}

	o := &Orchestrator{
		offloadables: make(chan message, 100),
		timeout:      time.Second * time.Duration(timeout),

		parser: newFileParser(file),
		param:  newParametrizer(),

		refs:    make(map[string]string),
		backend: b,
	}

	if !b.Ping() {
		panic("backend down, check connection")
	}

	log.Println("Backend up, continuing...")

	// Upload Wasm binaries
	o.uploadAll()
	// start routine that checks for messages in the channel
	go o.runTask()

	return o
}

func (o *Orchestrator) uploadAll() {
	names := make([]string, 0, len(o.parser.funcMetas))
	for name := range o.parser.funcMetas {
		names = append(names, name)
	}

	// check which refs are already uploaded
	remote, err := o.backend.GetStorage()
	if err != nil {
		panic(fmt.Errorf("error fetching shas: %w", err))
	}
	o.refsRemote = remote

	// checks for every annotated function if its uploaded, if not, loads and uploads
	for _, name := range names {
		_, err := o.ensureUploaded(name)
		if err != nil {
			panic(err)
		}
	}
}

func (o *Orchestrator) runTask() {
	for msg := range o.offloadables {

		ref, ok := o.refs[msg.offloadableName]
		if !ok {
			msg.result <- result{
				offloadable: msg.offloadableName,
				err:         fmt.Errorf("%s: no uploaded module, is the function marked with // offload?", msg.offloadableName),
			}
			o.wg.Done()
			continue
		}

		// offloading takes place here
		go func(m message) {
			defer o.wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
			defer cancel()

			r := o.run(ctx, m, ref)
			m.result <- r
		}(msg)
	}
	o.wg.Wait()
}

func (o *Orchestrator) run(ctx context.Context, m message, ref string) result {
	res := result{
		offloadable: m.offloadableName,
	}

	moduleName := m.offloadableName + ".wasm"

	stdout, stderr, status, err := o.backend.Run(ctx, ref, moduleName, m.argsSerialized)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			res.err = fmt.Errorf("%s timed out after %s", moduleName, o.timeout)
		} else {
			res.err = err
		}
		return res
	}
	if status != 0 {
		res.err = fmt.Errorf("%s failed with status %d: %s", moduleName, status, stderr)
		return res
	}
	res.output = stdout
	return res
}

func SubmitAll[T any](o *Orchestrator, funcName string, payloads ...any) ([]T, []error) {
	if funcName == "" || len(payloads) == 0 {
		panic("either name or payload(s) not specified")
	}

	meta, ok := o.parser.funcMetas[funcName]
	if !ok {
		panic(funcName + " is not marked with // offload")
	}

	results := make([]result, len(payloads))
	msgs := make(map[int]message, len(payloads))

	for i, payload := range payloads {
		if err := o.param.checkPayload(payload, meta); err != nil {
			results[i] = result{offloadable: funcName, err: err}
			log.Printf("submit %q failed: %v, continuing...", funcName, results[i].err)
			continue
		}

		serialized, err := o.param.encodeGob(payload)
		if err != nil {
			results[i] = result{offloadable: funcName, err: err}
			log.Printf("decode %q failed: %v, continuing...", funcName, err)
			continue
		}

		msgs[i] = message{
			offloadableName: funcName,
			argsSerialized:  serialized,
			result:          make(chan result, 1),
		}
	}

	for _, msg := range msgs {
		o.wg.Add(1)
		o.offloadables <- msg
	}

	for i, msg := range msgs {
		results[i] = <-msg.result
	}

	values := make([]T, len(results))
	errs := make([]error, len(results))

	for i := range results {
		v, err := decodeReturn[T](results[i])
		values[i] = v
		errs[i] = err
	}
	return values, errs
}

func Submit[T any](o *Orchestrator, funcName string, payload any) (T, error) {
	values, errs := SubmitAll[T](o, funcName, payload)
	return values[0], errs[0]
}

func Dispatch[T any](o *Orchestrator, funcName string, payload any) (chan T, chan error) {
	valCh := make(chan T, 1)
	errCh := make(chan error, 1)
	go func() {
		v, err := Submit[T](o, funcName, payload)
		valCh <- v
		errCh <- err
	}()
	return valCh, errCh
}

func DispatchAll[T any](o *Orchestrator, funcName string, payloads ...any) (chan []T, chan []error) {
	valCh := make(chan []T, 1)
	errCh := make(chan []error, 1)
	go func() {
		values, errs := SubmitAll[T](o, funcName, payloads...)
		valCh <- values
		errCh <- errs
	}()
	return valCh, errCh
}

func decodeReturn[R any](r result) (R, error) {
	var v R
	if r.err != nil {
		return v, r.err
	}

	err := gob.NewDecoder(bytes.NewReader(r.output)).Decode(&v)
	if err != nil {
		return v, err
	}
	return v, err
}

func (o *Orchestrator) Close() {
	close(o.offloadables)
	o.wg.Wait()
}

func loadWasm(name string) ([]byte, error) {
	dir, err := makeOffloadablesDir()
	if err != nil {
		return nil, err
	}

	return os.ReadFile(filepath.Join(dir, "offloadables", name+".wasm"))
}

func (o *Orchestrator) ensureUploaded(name string) (string, error) {
	if ref, ok := o.refs[name]; ok {
		return ref, nil
	}

	wasm, err := loadWasm(name)
	if err != nil {
		return "", fmt.Errorf("error loading wasm %s: %w", name, err)
	}

	if o.refsRemote == nil {
		o.refsRemote, err = o.backend.GetStorage()
		if err != nil {
			return "", fmt.Errorf("error fetching shas: %w", err)
		}
	}

	sum := sha256.Sum256(wasm)
	sha := hex.EncodeToString(sum[:])
	if o.refsRemote[sha] {
		log.Printf("%s already uploaded, in broker storage \n", name)
		ref := "sha256:" + sha
		o.refs[name] = ref
		return o.refs[name], nil
	}

	fmt.Println("Uploading...", name)
	ref, err := o.backend.Upload(wasm, name+".wasm")

	if err != nil {
		return "", fmt.Errorf("error uploading %s: %w", name, err)
	}

	o.refs[name] = ref
	return ref, nil

}
