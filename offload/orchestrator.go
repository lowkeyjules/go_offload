package offload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"wasi.team/client"
	wasimoffv1 "wasi.team/proto/v1"
)

type Orchestrator struct {
	// User config parameter
	Url     string
	File    string
	Timeout time.Duration

	// Objects used by Orchestrator
	Parser *Parser
	Param  *Parametrizer

	// Offloading helper
	Offloadables chan Message
	Refs         map[string]string
	RefsRemote   map[string]bool
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
		Offloadables: make(chan Message, 100),
		Url:          url,
		Timeout:      time.Second * time.Duration(timeout),
		Parser:       NewParser(file),
		Param:        NewParametrizer(),
		Refs:         make(map[string]string),
	}

	if !isBrokerUp(url) {
		panic("Broker down: check Broker connection")
	}

	o.wasimoff = client.NewWasimoffConnectRpcClient(http.DefaultClient, o.Url)
	log.Printf("connecting to Broker at %s ...", o.Url)

	o.uploadAll()

	// Starts a go routine which checks the channel for offloadables and offloads them
	go o.runTask()

	return o
}

func (o *Orchestrator) uploadAll() {
	names := make([]string, 0, len(o.Parser.FuncMetas))
	for name := range o.Parser.FuncMetas {
		names = append(names, name)
	}
	sort.Strings(names)

	remote, err := o.fetchStorage()
	if err != nil {
		panic(fmt.Errorf("error fetching shas: %w", err))
	}
	o.RefsRemote = remote

	for _, name := range names {
		if _, err := o.ensureUploaded(o.wasimoff, name); err != nil {
			panic(err)
		}
	}
}

func (o *Orchestrator) runTask() {
	ctx, cancel := context.WithTimeout(context.Background(), o.Timeout)
	defer cancel()

	for msg := range o.Offloadables {

		ref, ok := o.Refs[msg.Offloadable]
		if !ok {
			msg.result <- Result{
				Offloadable: msg.Offloadable,
				Args:        msg.ArgsSerialized,
				Err:         fmt.Errorf("%s: no uploaded module, is the function marked with // offload?", msg.Offloadable),
			}
			o.wg.Done()
			continue
		}

		file := &wasimoffv1.File{Ref: &ref}

		// offloading takes place here
		go func(m Message) {
			defer o.wg.Done()
			r := o.run(ctx, o.wasimoff, m, file)
			m.result <- r
		}(msg)
	}
	o.wg.Wait()
}

func (o *Orchestrator) run(ctx context.Context, wasimoff client.WasimoffClient, m Message, file *wasimoffv1.File) Result {
	result := Result{
		Offloadable: m.Offloadable,
		Args:        m.ArgsSerialized,
	}

	binary := m.Offloadable + ".wasm"
	request := wasimoffv1.Task_Wasip1_Request{
		Params: &wasimoffv1.Task_Wasip1_Params{
			Binary: file,
			Args:   []string{binary, result.Args},
		},
	}

	response, err := wasimoff.RunWasip1(ctx, &request)
	if err != nil {
		result.Err = err
		return result
	}

	if e := response.GetError(); e != "" {
		result.Err = fmt.Errorf("%s: %s", binary, e)
		return result
	}

	out := response.GetOk()
	if out == nil {
		result.Err = fmt.Errorf("%s: keine Ausgabe vom Broker", binary)
		return result
	}

	result.Stderr = strings.TrimSpace(string(out.GetStderr()))
	if status := out.GetStatus(); status != 0 {
		result.Err = fmt.Errorf("%s: exit status %d: %s", binary, status, result.Stderr)
		return result
	}

	result.Output = out.GetStdout()
	return result
}

func SubmitAll[T any](o *Orchestrator, name string, payloads ...any) ([]T, []error) {
	if name == "" || len(payloads) == 0 {
		panic("either name or payload(s) not specified")
	}

	meta, ok := o.Parser.FuncMetas[name]
	if !ok {
		panic(name + " is not marked with // offload")
	}

	results := make([]Result, len(payloads))
	msgs := make(map[int]Message, len(payloads))

	for i, payload := range payloads {
		if err := o.Param.CheckPayload(payload, meta); err != nil {
			results[i] = Result{Offloadable: name, Err: err}
			log.Printf("submit %q failed: %v, continuing", name, results[i].Err)
			continue
		}

		serialized, err := o.Param.EncodeGob(payload)
		if err != nil {
			results[i] = Result{Offloadable: name, Err: err}
			log.Printf("decode %q failed: %v, continuing", name, err)
			continue
		}

		msgs[i] = Message{
			Offloadable:    name,
			ArgsSerialized: serialized,
			result:         make(chan Result, 1),
		}
	}

	for _, msg := range msgs {
		o.wg.Add(1)
		o.Offloadables <- msg
	}

	for i, msg := range msgs {
		results[i] = <-msg.result
	}

	values := make([]T, len(results))
	errs := make([]error, len(results))

	for i := range results {
		v, err := decodeReturn[T](results[i])
		if err != nil {
			log.Printf("offload failed for call %q, %d, %v", name, i, err)
		}
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

func decodeReturn[R any](r Result) (R, error) {
	var v R
	if r.Err != nil {
		return v, r.Err
	}

	var wrapped wireReturn
	if err := gob.NewDecoder(bytes.NewReader(r.Output)).Decode(&wrapped); err != nil {
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
	close(o.Offloadables)
	o.wg.Wait()
}

func loadWasm(name string) ([]byte, error) {
	return os.ReadFile("./offload/offloadables/" + name + ".wasm")
}

func (o *Orchestrator) ensureUploaded(wasimoff client.WasimoffClient, name string) (string, error) {
	if ref, ok := o.Refs[name]; ok {
		return ref, nil
	}

	wasm, err := loadWasm(name)
	if err != nil {
		return "", fmt.Errorf("error loading wasm %s: %w", name, err)
	}

	if o.RefsRemote == nil {
		o.RefsRemote, err = o.fetchStorage()
		if err != nil {
			return "", fmt.Errorf("error fetching shas: %w", err)
		}
	}

	sum := sha256.Sum256(wasm)
	sha := hex.EncodeToString(sum[:])
	if o.RefsRemote[sha] {
		log.Printf("%s already uploaded, in broker storage \n", name)
		ref := "sha256:" + sha
		o.Refs[name] = ref
		return o.Refs[name], nil
	}

	fmt.Println("Uploading...", name)
	ref, err := wasimoff.Upload(wasm, name+".wasm")

	if err != nil {
		return "", fmt.Errorf("error uploading %s: %w", name, err)
	}

	o.Refs[name] = ref
	return ref, nil

}

func (o *Orchestrator) fetchStorage() (map[string]bool, error) {
	urlStorage := o.Url + "/api/storage"

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
