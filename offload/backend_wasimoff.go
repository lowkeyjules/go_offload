package offload

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"wasi.team/client"
	wasimoffv1 "wasi.team/proto/v1"
)

type backendWasimoff struct {
	url    string
	client client.WasimoffClient
}

// newBackendWasimoff takes broker URL as the input and returns a backend object.
func newBackendWasimoff(url string) *backendWasimoff {
	return &backendWasimoff{url: url, client: client.NewWasimoffConnectRpcClient(http.DefaultClient, url)}
}

// Ping checks for response status
func (b *backendWasimoff) Ping() bool {
	resp, err := http.Get(b.url)
	if err != nil {
		return false
	}

	defer resp.Body.Close()

	return resp.StatusCode == 200
}

func (b *backendWasimoff) GetStorage() (map[string]bool, error) {
	// build wasimoff specific storage url
	urlStorage := b.url + "/api/storage"

	resp, err := http.Get(urlStorage)
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("storage: HTTP %d", resp.StatusCode)
	}

	// body returns JSON object
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// builds string array from all refs in from the JSON
	var list []string
	if err := json.Unmarshal(body, &list); err != nil {
		return nil, fmt.Errorf("storage response unknown: %s", body)
	}

	// remove prefix to remove inconsistencies
	for i := range list {
		list[i] = strings.TrimPrefix(list[i], "sha256:")
	}

	// list of SHA256
	// TODO check if list is necessary here
	set := make(map[string]bool, len(list))
	for _, entry := range list {
		set[entry] = true
	}
	return set, nil

}

func (b *backendWasimoff) Upload(wasm []byte, name string) (ref string, err error) {
	return b.client.Upload(wasm, name)
}

func (b *backendWasimoff) Run(ctx context.Context, ref string, moduleName string, args string) (stdout []byte, stderr []byte, status int32, err error) {
	file := &wasimoffv1.File{Ref: &ref}

	request := wasimoffv1.Task_Wasip1_Request{
		Params: &wasimoffv1.Task_Wasip1_Params{
			Binary: file,
			Args:   []string{moduleName, args},
		},
	}

	response, err := b.client.RunWasip1(ctx, &request)
	// check for initial error when running wasm
	if err != nil {
		return nil, nil, 0, err
	}

	// Check for error
	e := response.GetError()
	if e != "" {
		return nil, nil, 0, errors.New(e)
	}
	output := response.GetOk()
	if output == nil {
		return nil, nil, 0, fmt.Errorf("no output from broker")
	}
	return output.GetStdout(), output.GetStderr(), output.GetStatus(), nil
}
