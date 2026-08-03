package offload

type Message struct {
	Offloadable    string
	ArgsSerialized string
	result         chan Result
}
