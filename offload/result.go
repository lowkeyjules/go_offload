package offload

type result struct {
	offloadable string
	args        string
	output      []byte
	stderr      string
	err         error
}

type wireReturn struct {
	ReturnEncoded string
	ErrMsg        string
}
