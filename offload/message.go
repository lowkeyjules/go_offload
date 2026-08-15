package offload

type message struct {
	offloadable    string
	argsSerialized string
	result         chan result
}
