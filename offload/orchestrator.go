package offload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"wasi.team/client"
	wasimoffv1 "wasi.team/proto/v1"
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
	refs         map[string]string
	refsRemote   map[string]bool
	wasimoff     client.WasimoffClient
	wg           sync.WaitGroup
}

// OpenOffload is the core of the offloading programm.
// It creates an orchestrator object which creates the Wasm modules
// as well as offloads functions when called.
// While the naming can be confusing, it has been deliberately chosen, to signal the user that
// the channel, which has been opened for offloading, must be closed.
func OpenOffload(url string, timeout int, file string) *Orchestrator {

	if file == "" {
		file = "main.go"
	}

	if timeout < 5 {
		timeout = 5
	}

	o := &Orchestrator{
		offloadables: make(chan message, 100),
		url:          url,
		timeout:      time.Second * time.Duration(timeout),
		parser:       newFileParser(file),
		param:        newParametrizer(),
		refs:         make(map[string]string),
	}

	if !isBrokerUp(url) {
		panic("Broker down: check Broker connection")
	}

	o.wasimoff = client.NewWasimoffConnectRpcClient(http.DefaultClient, o.url)
	log.Printf("connecting to Broker at %s ...", o.url)

	o.uploadAll()

	// Starts a go routine which checks the channel for offloadables and offloads them
	go o.runTask()

	return o
}

func (o *Orchestrator) uploadAll() {
	names := make([]string, 0, len(o.parser.funcMetas))
	for name := range o.parser.funcMetas {
		names = append(names, name)
	}
	sort.Strings(names)

	remote, err := o.fetchStorage()
	if err != nil {
		panic(fmt.Errorf("error fetching shas: %w", err))
	}
	o.refsRemote = remote

	for _, name := range names {
		if _, err := o.ensureUploaded(o.wasimoff, name); err != nil {
			panic(err)
		}
	}
}

func (o *Orchestrator) runTask() {
	for msg := range o.offloadables {

		ref, ok := o.refs[msg.offloadable]
		if !ok {
			msg.result <- result{
				offloadable: msg.offloadable,
				args:        msg.argsSerialized,
				err:         fmt.Errorf("%s: no uploaded module, is the function marked with // offload?", msg.offloadable),
			}
			o.wg.Done()
			continue
		}

		file := &wasimoffv1.File{Ref: &ref}

		// offloading takes place here
		go func(m message) {
			defer o.wg.Done()

			ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
			defer cancel()

			r := o.run(ctx, o.wasimoff, m, file)
			m.result <- r
		}(msg)
	}
	o.wg.Wait()
}

func (o *Orchestrator) run(ctx context.Context, wasimoff client.WasimoffClient, m message, file *wasimoffv1.File) result {
	result := result{
		offloadable: m.offloadable,
		args:        m.argsSerialized,
	}

	binary := m.offloadable + ".wasm"
	request := wasimoffv1.Task_Wasip1_Request{
		Params: &wasimoffv1.Task_Wasip1_Params{
			Binary: file,
			Args:   []string{binary, result.args},
		},
	}

	response, err := wasimoff.RunWasip1(ctx, &request)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.err = fmt.Errorf("%s timed out after %s", binary, o.timeout)
		} else {
			result.err = err
		}
		return result
	}

	if e := response.GetError(); e != "" {
		result.err = fmt.Errorf("%s: %s", binary, e)
		return result
	}

	out := response.GetOk()
	if out == nil {
		result.err = fmt.Errorf("%s: no output from broker", binary)
		return result
	}

	result.stderr = strings.TrimSpace(string(out.GetStderr()))
	if status := out.GetStatus(); status != 0 {
		result.err = fmt.Errorf("%s: exit status %d: %s", binary, status, result.stderr)
		return result
	}

	result.output = out.GetStdout()
	return result
}

func SubmitAll[T any](o *Orchestrator, name string, payloads ...any) ([]T, []error) {
	if name == "" || len(payloads) == 0 {
		panic("either name or payload(s) not specified")
	}

	meta, ok := o.parser.funcMetas[name]
	if !ok {
		panic(name + " is not marked with // offload")
	}

	results := make([]result, len(payloads))
	msgs := make(map[int]message, len(payloads))

	for i, payload := range payloads {
		if err := o.param.checkPayload(payload, meta); err != nil {
			results[i] = result{offloadable: name, err: err}
			log.Printf("submit %q failed: %v, continuing", name, results[i].err)
			continue
		}

		serialized, err := o.param.encodeGob(payload)
		if err != nil {
			results[i] = result{offloadable: name, err: err}
			log.Printf("decode %q failed: %v, continuing", name, err)
			continue
		}

		msgs[i] = message{
			offloadable:    name,
			argsSerialized: serialized,
			result:         make(chan result, 1),
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

func Submit[T any](o *Orchestrator, name string, payload any) (T, error) {
	values, errs := SubmitAll[T](o, name, payload)
	return values[0], errs[0]
}

func Dispatch[T any](o *Orchestrator, name string, payload any) (chan T, chan error) {
	valCh := make(chan T, 1)
	errCh := make(chan error, 1)
	go func() {
		v, err := Submit[T](o, name, payload)
		valCh <- v
		errCh <- err
	}()
	return valCh, errCh
}

func DispatchAll[T any](o *Orchestrator, name string, payloads ...any) (chan []T, chan []error) {
	valCh := make(chan []T, 1)
	errCh := make(chan []error, 1)
	go func() {
		values, errs := SubmitAll[T](o, name, payloads...)
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

	var wrapped wireReturn
	if err := gob.NewDecoder(bytes.NewReader(r.output)).Decode(&wrapped); err != nil {
		return v, fmt.Errorf("decode wrapper gob: %w", err)
	}

	if wrapped.ErrMsg != "" {
		return v, fmt.Errorf("%s", wrapped.ErrMsg)
	}

	inner, err := base64.StdEncoding.DecodeString(wrapped.ReturnEncoded)
	if err != nil {
		return v, err
	}

	err = gob.NewDecoder(bytes.NewReader(inner)).Decode(&v)
	return v, err
}

func (o *Orchestrator) Close() {
	close(o.offloadables)
	o.wg.Wait()
}

func loadWasm(name string) ([]byte, error) {
	dir, err := offloadblesDir()
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(dir, "offloadables", name+".wasm"))
}

func (o *Orchestrator) ensureUploaded(wasimoff client.WasimoffClient, name string) (string, error) {
	if ref, ok := o.refs[name]; ok {
		return ref, nil
	}

	wasm, err := loadWasm(name)
	if err != nil {
		return "", fmt.Errorf("error loading wasm %s: %w", name, err)
	}

	if o.refsRemote == nil {
		o.refsRemote, err = o.fetchStorage()
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
	ref, err := wasimoff.Upload(wasm, name+".wasm")

	if err != nil {
		return "", fmt.Errorf("error uploading %s: %w", name, err)
	}

	o.refs[name] = ref
	return ref, nil

}

func (o *Orchestrator) fetchStorage() (map[string]bool, error) {
	urlStorage := o.url + "/api/storage"

	resp, err := http.Get(urlStorage)
	if err != nil {
		return nil, fmt.Errorf("storage abfragen: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("storage: HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var list []string
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("storage-Antwort unknown: %.80s", body)
	}

	for i := range list {
		list[i] = strings.TrimPrefix(list[i], "sha256:")
	}

	set := make(map[string]bool, len(list))
	for _, entry := range list {
		set[entry] = true
	}
	return set, nil

}

func isBrokerUp(url string) bool {
	resp, err := http.Get(url)
	if err != nil {
		return false
	}
	return resp.StatusCode == 200
}
