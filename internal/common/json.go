package common

import (
	"fmt"
	"io"

	"github.com/bytedance/sonic"
)

// WriteJSON writes v as one JSON line.
func WriteJSON(w io.Writer, v any) error {
	data, err := sonic.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(data))
	return err
}
