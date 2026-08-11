package canvasplan

import (
	"encoding/json"
	"fmt"
	"io"
)

const maxContractBytes = 16 << 20

func DecodePlan(reader io.Reader) (Plan, error) {
	var plan Plan
	if err := decodeStrictJSON(reader, &plan); err != nil {
		return Plan{}, fmt.Errorf("decode CanvasPlan: %w", err)
	}
	return NormalizePlan(plan)
}

func DecodeResolvedMedia(reader io.Reader) (ResolvedMediaSet, error) {
	var resolved ResolvedMediaSet
	if err := decodeStrictJSON(reader, &resolved); err != nil {
		return ResolvedMediaSet{}, fmt.Errorf("decode resolved media: %w", err)
	}
	return NormalizeResolvedMedia(resolved)
}

func decodeStrictJSON(reader io.Reader, target any) error {
	if reader == nil {
		return fmt.Errorf("JSON input is required")
	}
	limited := io.LimitReader(reader, maxContractBytes+1)
	payload, err := io.ReadAll(limited)
	if err != nil {
		return err
	}
	if len(payload) > maxContractBytes {
		return fmt.Errorf("JSON input exceeds %d bytes", maxContractBytes)
	}
	decoder := json.NewDecoder(bytesReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("decode trailing JSON: %w", err)
	}
	return fmt.Errorf("JSON input must contain exactly one value")
}

type byteReader struct {
	data []byte
	off  int
}

func bytesReader(data []byte) *byteReader {
	return &byteReader{data: data}
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}
