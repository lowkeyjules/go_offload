package offload

import "context"

// Backend is an interface for implementing different backends like Wasimoff.
type Backend interface {

	// Ping calls Backend-Url and checks for connectivity
	// true if reachable, else false
	Ping() bool

	// GetStorage makes an API call to check for currently uploaded Wasm modules.
	// Returns map containing ref and bool value whether is already uploaded to backend.
	GetStorage() (map[string]bool, error)

	// Upload uploads current Wasm binary to backend
	Upload(wasm []byte, name string) (ref string, err error)

	// Run executes Wasm binaries on computational backend
	// Currently very Wasimoff-specific, might get adjusted in the future.
	Run(ctx context.Context, ref string, binary string, args string) (stdout []byte, stderr []byte, status int32, err error)
}
