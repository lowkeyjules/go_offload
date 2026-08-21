package offload

type message struct {
	offloadableName string
	argsSerialized  string
	result          chan result
}
