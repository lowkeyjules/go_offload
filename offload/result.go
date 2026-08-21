package offload

// result struct contains following fields:
// offloadable - name of the currently offloaded function,
// output - encoded return value from backend,
// err - error passed from backend
type result struct {
	offloadable string
	output      []byte
	err         error
}
