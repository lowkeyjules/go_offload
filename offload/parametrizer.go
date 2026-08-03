package offload

import (
	"bytes"
	"encoding/base64"
	"encoding/gob"
	"fmt"
	"reflect"
)

type Parametrizer struct {
	encoded string
}

func NewParametrizer() *Parametrizer {
	return &Parametrizer{}
}

func (p *Parametrizer) EncodeGob(input any) (string, error) {
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(input); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func (p *Parametrizer) CheckPayload(payload any, meta FuncMeta) error {
	t := reflect.TypeOf(payload)
	for t != nil && t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == nil || t.Kind() != reflect.Struct {
		return fmt.Errorf("payload muss ein Struct sein")
	}
	if t.Name() != meta.Payload {
		return fmt.Errorf("erwartet Payload %s, bekommen %s", meta.Payload, t.Name())
	}
	return nil
}
