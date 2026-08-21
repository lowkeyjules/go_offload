package offload

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"reflect"
)

type parametrizer struct {
}

// parametrizer checks and encodes the input. Input is checked for being a struct, gob encodes into gob/base64.
func newParametrizer() *parametrizer {
	return &parametrizer{}
}

// encodeGob encodes, as the name suggests, the input into Gob and converts it into a base64 string
func (p *parametrizer) encodeGob(input any) (string, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(input); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func (p *parametrizer) checkPayload(payload any, meta funcMeta) error {
	t := reflect.TypeOf(payload)
	// go convention for flattening pointer
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return fmt.Errorf("payload must be a struct")
	}
	if t.Name() != meta.payload {
		return fmt.Errorf("expected payload: %s, received: %s", meta.payload, t.Name())
	}
	return nil
}
