package offload

type Result struct {
	Offloadable string
	Args        string
	Output      []byte
	Stderr      string
	Err         error
}

type wireReturn struct {
	ReturnEncoded string
	ErrMsg        string
}
