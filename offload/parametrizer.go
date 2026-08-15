package offload

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"reflect"
)

type parametrizer struct {
	encoded string
}

func newParametrizer() *parametrizer {
	return &parametrizer{}
}

func (p *parametrizer) encodeGob(input any) (string, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(input); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func (p *parametrizer) checkPayload(payload any, meta funcMeta) error {
	t := reflect.TypeOf(payload)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return fmt.Errorf("payload muss ein Struct sein")
	}
	if t.Name() != meta.payload {
		return fmt.Errorf("expected payload: %s, received: %s", meta.payload, t.Name())
	}
	return nil
}
