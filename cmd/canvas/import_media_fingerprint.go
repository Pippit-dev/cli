package canvas

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Pippit-dev/pippit-cli/internal/canvasplan"
)

const (
	libTVPNGAIGCFingerprintPrefix = "libtv-png-aigc-v1:"
	rawMediaFingerprintPrefix     = "raw-sha256:"
	maxCanonicalITXtChunkBytes    = 1 << 20
	libTVAIGCProducer             = "001191110105MACJ6K1C8A10001"
)

var pngFileSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

type importMediaFileIdentity struct {
	RawSHA256          string
	ContentFingerprint string
	ByteSize           int64
}

type legacyCheckpointMediaIdentity struct {
	ContentFingerprint string
	ByteSize           int64
}

type importMediaCountingReader struct {
	reader io.Reader
	count  int64
}

func (reader *importMediaCountingReader) Read(payload []byte) (int, error) {
	count, err := reader.reader.Read(payload)
	reader.count += int64(count)
	return count, err
}

// inspectImportMediaFile derives the byte size, raw digest, and the narrowly
// canonicalized LibTV PNG identity from one no-follow file descriptor. The
// path is checked against the descriptor before and after the stream is read,
// so callers never combine facts derived from different path targets.
func inspectImportMediaFile(path string) (importMediaFileIdentity, error) {
	file, identity, err := openInspectedImportMediaFile(path)
	if file != nil {
		_ = file.Close()
	}
	return identity, err
}

// openInspectedImportMediaFile leaves the verified descriptor open for the
// caller. This lets the upload path seek and stream the exact inode whose
// digest and canonical fingerprint were checked, without reopening by path.
func openInspectedImportMediaFile(path string) (*os.File, importMediaFileIdentity, error) {
	file, err := openImportMediaNoFollow(path)
	if err != nil {
		return nil, importMediaFileIdentity{}, err
	}
	initial, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, importMediaFileIdentity{}, fmt.Errorf("inspect opened import media: %w", err)
	}
	identity, inspectErr := inspectImportMediaContent(file)
	stableErr := validateStableImportMediaFile(path, file, initial)
	if inspectErr != nil {
		_ = file.Close()
		return nil, importMediaFileIdentity{}, inspectErr
	}
	if stableErr != nil {
		_ = file.Close()
		return nil, importMediaFileIdentity{}, stableErr
	}
	if identity.ByteSize != initial.Size() {
		_ = file.Close()
		return nil, importMediaFileIdentity{}, fmt.Errorf("import media size changed while it was inspected")
	}
	return file, identity, nil
}

func validateStableImportMediaFile(path string, file *os.File, initial os.FileInfo) error {
	opened, err := file.Stat()
	if err != nil {
		return fmt.Errorf("reinspect opened import media: %w", err)
	}
	current, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("reinspect import media path: %w", err)
	}
	if fileInfoIsImportMediaLinkLike(current) || !current.Mode().IsRegular() ||
		!opened.Mode().IsRegular() || !os.SameFile(initial, opened) || !os.SameFile(opened, current) {
		return fmt.Errorf("import media path changed while it was inspected: %s", path)
	}
	if initial.Size() != opened.Size() || !initial.ModTime().Equal(opened.ModTime()) {
		return fmt.Errorf("import media content changed while it was inspected: %s", path)
	}
	return nil
}

func inspectImportMediaContent(reader io.Reader) (importMediaFileIdentity, error) {
	counting := &importMediaCountingReader{reader: reader}
	rawHash := sha256.New()
	stream := io.TeeReader(counting, rawHash)
	signature := make([]byte, len(pngFileSignature))
	read, err := io.ReadFull(stream, signature)
	if err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			return importMediaFileIdentity{}, fmt.Errorf("read import media signature: %w", err)
		}
		return rawImportMediaIdentity(rawHash, counting.count), nil
	}
	if read != len(pngFileSignature) || !bytes.Equal(signature, pngFileSignature) {
		if _, err := io.Copy(io.Discard, stream); err != nil {
			return importMediaFileIdentity{}, fmt.Errorf("hash import media: %w", err)
		}
		return rawImportMediaIdentity(rawHash, counting.count), nil
	}
	canonicalDigest, normalizedAIGC, err := canonicalLibTVPNGStream(stream)
	if err != nil {
		return importMediaFileIdentity{}, err
	}
	rawDigest := hex.EncodeToString(rawHash.Sum(nil))
	fingerprint := rawMediaFingerprintPrefix + rawDigest
	if normalizedAIGC {
		fingerprint = libTVPNGAIGCFingerprintPrefix + canonicalDigest
	}
	return importMediaFileIdentity{
		RawSHA256:          rawDigest,
		ContentFingerprint: fingerprint,
		ByteSize:           counting.count,
	}, nil
}

func rawImportMediaIdentity(rawHash hash.Hash, byteSize int64) importMediaFileIdentity {
	digest := hex.EncodeToString(rawHash.Sum(nil))
	return importMediaFileIdentity{
		RawSHA256:          digest,
		ContentFingerprint: rawMediaFingerprintPrefix + digest,
		ByteSize:           byteSize,
	}
}

func importMediaContentFingerprint(path, rawSHA256 string) (string, error) {
	identity, err := inspectImportMediaFile(path)
	if err != nil {
		return "", err
	}
	if identity.RawSHA256 != rawSHA256 {
		return "", fmt.Errorf("import media raw SHA-256 changed before fingerprinting")
	}
	return identity.ContentFingerprint, nil
}

func canonicalLibTVPNGFingerprint(payload []byte) (string, error) {
	identity, err := inspectImportMediaContent(bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	if len(payload) < len(pngFileSignature) || !bytes.Equal(payload[:len(pngFileSignature)], pngFileSignature) {
		return "", fmt.Errorf("PNG signature is invalid")
	}
	return identity.ContentFingerprint, nil
}

func canonicalLibTVPNGStream(reader io.Reader) (string, bool, error) {
	canonicalHash := sha256.New()
	_, _ = canonicalHash.Write(pngFileSignature)
	chunkIndex := 0
	seenIHDR := false
	seenPLTE := false
	seenIDAT := false
	endedIDAT := false
	colorType := byte(0xff)
	aigcChunkCount := 0
	normalizedAIGCCount := 0
	for {
		var header [8]byte
		if _, err := io.ReadFull(reader, header[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				return "", false, fmt.Errorf("PNG is missing IEND or has a truncated chunk header")
			}
			return "", false, fmt.Errorf("read PNG chunk header: %w", err)
		}
		length := uint64(binary.BigEndian.Uint32(header[:4]))
		chunkType := header[4:]
		chunkName := string(chunkType)
		if err := validatePNGChunkType(chunkType); err != nil {
			return "", false, err
		}
		if chunkIndex == 0 && chunkName != "IHDR" {
			return "", false, fmt.Errorf("PNG IHDR must be the first chunk")
		}
		if chunkName != "IDAT" && seenIDAT {
			endedIDAT = true
		}
		switch chunkName {
		case "IHDR":
			if seenIHDR || chunkIndex != 0 || length != 13 {
				return "", false, fmt.Errorf("PNG must contain one 13-byte IHDR as its first chunk")
			}
			seenIHDR = true
		case "PLTE":
			if seenPLTE || seenIDAT || length == 0 || length > 768 || length%3 != 0 {
				return "", false, fmt.Errorf("PNG has an invalid or misplaced PLTE chunk")
			}
			seenPLTE = true
		case "IDAT":
			if !seenIHDR || endedIDAT {
				return "", false, fmt.Errorf("PNG IDAT chunks must be consecutive and follow IHDR")
			}
			if colorType == 3 && !seenPLTE {
				return "", false, fmt.Errorf("indexed PNG is missing PLTE before IDAT")
			}
			seenIDAT = true
		case "IEND":
			if !seenIDAT || length != 0 {
				return "", false, fmt.Errorf("PNG IEND must be empty and follow IDAT")
			}
		default:
			if chunkType[0]&0x20 == 0 {
				return "", false, fmt.Errorf("PNG contains unknown critical chunk %q", chunkName)
			}
		}

		bufferChunk := chunkName == "IHDR" || (chunkName == "iTXt" && length <= maxCanonicalITXtChunkBytes)
		if bufferChunk {
			chunkData := make([]byte, int(length))
			if _, err := io.ReadFull(reader, chunkData); err != nil {
				return "", false, fmt.Errorf("read PNG chunk %q: %w", chunkName, err)
			}
			var crcBytes [4]byte
			if _, err := io.ReadFull(reader, crcBytes[:]); err != nil {
				return "", false, fmt.Errorf("read PNG chunk %q CRC: %w", chunkName, err)
			}
			if err := validatePNGChunkCRC(chunkType, chunkData, crcBytes); err != nil {
				return "", false, err
			}
			if chunkName == "IHDR" {
				var err error
				colorType, err = validatePNGIHDR(chunkData)
				if err != nil {
					return "", false, err
				}
			}
			canonicalData := chunkData
			matched := false
			if chunkName == "iTXt" {
				if isLibTVAIGCITXt(chunkData) {
					aigcChunkCount++
				}
				canonicalData, matched = normalizeLibTVAIGCITXt(chunkData)
				if matched {
					normalizedAIGCCount++
				}
			}
			writeCanonicalPNGChunk(canonicalHash, chunkType, canonicalData)
		} else {
			_, _ = canonicalHash.Write(header[:])
			crc := crc32.NewIEEE()
			_, _ = crc.Write(chunkType)
			chunkWriter := io.MultiWriter(canonicalHash, crc)
			remaining := int64(length)
			if chunkName == "iTXt" {
				prefixSize := int64(len("AIGC\x00"))
				if remaining < prefixSize {
					prefixSize = remaining
				}
				prefix := make([]byte, int(prefixSize))
				if _, err := io.ReadFull(reader, prefix); err != nil {
					return "", false, fmt.Errorf("read PNG chunk %q prefix: %w", chunkName, err)
				}
				_, _ = chunkWriter.Write(prefix)
				remaining -= prefixSize
				if bytes.Equal(prefix, []byte("AIGC\x00")) {
					aigcChunkCount++
				}
			}
			if _, err := io.CopyN(chunkWriter, reader, remaining); err != nil {
				return "", false, fmt.Errorf("read PNG chunk %q: %w", chunkName, err)
			}
			var crcBytes [4]byte
			if _, err := io.ReadFull(reader, crcBytes[:]); err != nil {
				return "", false, fmt.Errorf("read PNG chunk %q CRC: %w", chunkName, err)
			}
			if crc.Sum32() != binary.BigEndian.Uint32(crcBytes[:]) {
				return "", false, fmt.Errorf("PNG chunk %q has an invalid CRC", chunkName)
			}
			_, _ = canonicalHash.Write(crcBytes[:])
		}
		chunkIndex++
		if chunkName == "IEND" {
			var trailing [1]byte
			if _, err := io.ReadFull(reader, trailing[:]); err == nil {
				return "", false, fmt.Errorf("PNG contains trailing bytes after IEND")
			} else if err != io.EOF {
				return "", false, fmt.Errorf("inspect PNG trailing bytes: %w", err)
			}
			break
		}
	}
	if !seenIHDR || !seenIDAT {
		return "", false, fmt.Errorf("PNG is missing IHDR or IDAT")
	}
	if (colorType == 0 || colorType == 4) && seenPLTE {
		return "", false, fmt.Errorf("grayscale PNG must not contain PLTE")
	}
	return hex.EncodeToString(canonicalHash.Sum(nil)), aigcChunkCount == 1 && normalizedAIGCCount == 1, nil
}

func validatePNGChunkType(chunkType []byte) error {
	for _, value := range chunkType {
		if (value < 'A' || value > 'Z') && (value < 'a' || value > 'z') {
			return fmt.Errorf("PNG chunk type %q is invalid", string(chunkType))
		}
	}
	if chunkType[2]&0x20 != 0 {
		return fmt.Errorf("PNG chunk type %q has an invalid reserved bit", string(chunkType))
	}
	return nil
}

func validatePNGChunkCRC(chunkType, data []byte, crcBytes [4]byte) error {
	crc := crc32.NewIEEE()
	_, _ = crc.Write(chunkType)
	_, _ = crc.Write(data)
	if crc.Sum32() != binary.BigEndian.Uint32(crcBytes[:]) {
		return fmt.Errorf("PNG chunk %q has an invalid CRC", string(chunkType))
	}
	return nil
}

func validatePNGIHDR(data []byte) (byte, error) {
	width := binary.BigEndian.Uint32(data[:4])
	height := binary.BigEndian.Uint32(data[4:8])
	bitDepth := data[8]
	colorType := data[9]
	validDepth := map[byte]map[byte]bool{
		0: {1: true, 2: true, 4: true, 8: true, 16: true},
		2: {8: true, 16: true},
		3: {1: true, 2: true, 4: true, 8: true},
		4: {8: true, 16: true},
		6: {8: true, 16: true},
	}
	if width == 0 || height == 0 || !validDepth[colorType][bitDepth] ||
		data[10] != 0 || data[11] != 0 || data[12] > 1 {
		return 0, fmt.Errorf("PNG IHDR contains invalid dimensions or encoding fields")
	}
	return colorType, nil
}

func writeCanonicalPNGChunk(destination hash.Hash, chunkType, data []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(data)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write(chunkType)
	_, _ = destination.Write(data)
	crc := crc32.NewIEEE()
	_, _ = crc.Write(chunkType)
	_, _ = crc.Write(data)
	var checksum [4]byte
	binary.BigEndian.PutUint32(checksum[:], crc.Sum32())
	_, _ = destination.Write(checksum[:])
}

// normalizeLibTVAIGCITXt only recognizes the observed uncompressed LibTV
// schema: keyword AIGC, empty language fields, and its exact seven string
// fields. Any old, malformed, compressed, or future schema is kept byte-for-
// byte in the fingerprint instead of blocking an otherwise valid PNG.
func normalizeLibTVAIGCITXt(data []byte) ([]byte, bool) {
	header := []byte("AIGC\x00\x00\x00\x00\x00")
	if !bytes.HasPrefix(data, header) {
		return data, false
	}
	normalizedJSON, ok := normalizeLibTVAIGCJSON(data[len(header):])
	if !ok {
		return data, false
	}
	result := make([]byte, 0, len(header)+len(normalizedJSON))
	result = append(result, header...)
	result = append(result, normalizedJSON...)
	return result, true
}

func isLibTVAIGCITXt(data []byte) bool {
	return bytes.HasPrefix(data, []byte("AIGC\x00"))
}

func normalizeLibTVAIGCJSON(payload []byte) ([]byte, bool) {
	if !json.Valid(payload) {
		return payload, false
	}
	position := skipJSONWhitespace(payload, 0)
	if position >= len(payload) || payload[position] != '{' {
		return payload, false
	}
	position++
	cursor := 0
	fields := make(map[string]string, 7)
	var normalized bytes.Buffer
	for {
		position = skipJSONWhitespace(payload, position)
		if position >= len(payload) || payload[position] == '}' {
			break
		}
		keyStart := position
		keyEnd, err := scanJSONStringEnd(payload, keyStart)
		if err != nil {
			return payload, false
		}
		var key string
		if err := json.Unmarshal(payload[keyStart:keyEnd], &key); err != nil {
			return payload, false
		}
		if _, duplicate := fields[key]; duplicate || !knownLibTVAIGCField(key) {
			return payload, false
		}
		position = skipJSONWhitespace(payload, keyEnd)
		if position >= len(payload) || payload[position] != ':' {
			return payload, false
		}
		valueStart := skipJSONWhitespace(payload, position+1)
		valueEnd, err := scanJSONValueEnd(payload, valueStart)
		if err != nil {
			return payload, false
		}
		if valueStart >= len(payload) || payload[valueStart] != '"' {
			return payload, false
		}
		var value string
		if err := json.Unmarshal(payload[valueStart:valueEnd], &value); err != nil {
			return payload, false
		}
		fields[key] = value
		if key == "ProduceID" || key == "PropagateID" {
			normalized.Write(payload[cursor:valueStart])
			if key == "ProduceID" {
				normalized.WriteString(`"__pippit_normalized_produce_id__"`)
			} else {
				normalized.WriteString(`"__pippit_normalized_propagate_id__"`)
			}
			cursor = valueEnd
		}
		position = skipJSONWhitespace(payload, valueEnd)
		if position < len(payload) && payload[position] == ',' {
			position++
			continue
		}
		if position < len(payload) && payload[position] == '}' {
			break
		}
		return payload, false
	}
	if len(fields) != 7 || fields["Label"] != "1" ||
		fields["ContentProducer"] != libTVAIGCProducer ||
		fields["ContentPropagator"] != libTVAIGCProducer ||
		fields["ReservedCode1"] != "" || fields["ReservedCode2"] != "" ||
		fields["ProduceID"] != fields["PropagateID"] || !validLibTVAIGCID(fields["ProduceID"]) {
		return payload, false
	}
	normalized.Write(payload[cursor:])
	return normalized.Bytes(), true
}

func knownLibTVAIGCField(key string) bool {
	switch key {
	case "Label", "ContentProducer", "ProduceID", "ReservedCode1",
		"ContentPropagator", "PropagateID", "ReservedCode2":
		return true
	default:
		return false
	}
}

func validLibTVAIGCID(value string) bool {
	if len(value) != len("libtv")+32 || !strings.HasPrefix(value, "libtv") {
		return false
	}
	for _, character := range value[len("libtv"):] {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func scanJSONStringEnd(payload []byte, start int) (int, error) {
	if start >= len(payload) || payload[start] != '"' {
		return 0, fmt.Errorf("expected a JSON string")
	}
	for position := start + 1; position < len(payload); position++ {
		switch payload[position] {
		case '\\':
			position++
			if position >= len(payload) {
				return 0, fmt.Errorf("JSON string has a truncated escape")
			}
		case '"':
			return position + 1, nil
		}
	}
	return 0, fmt.Errorf("JSON string is unterminated")
}

func scanJSONValueEnd(payload []byte, start int) (int, error) {
	if start >= len(payload) {
		return 0, fmt.Errorf("JSON value is missing")
	}
	if payload[start] == '"' {
		return scanJSONStringEnd(payload, start)
	}
	if payload[start] == '{' || payload[start] == '[' {
		stack := []byte{matchingJSONDelimiter(payload[start])}
		for position := start + 1; position < len(payload); position++ {
			if payload[position] == '"' {
				end, err := scanJSONStringEnd(payload, position)
				if err != nil {
					return 0, err
				}
				position = end - 1
				continue
			}
			switch payload[position] {
			case '{', '[':
				stack = append(stack, matchingJSONDelimiter(payload[position]))
			case '}', ']':
				if len(stack) == 0 || payload[position] != stack[len(stack)-1] {
					return 0, fmt.Errorf("JSON has mismatched delimiters")
				}
				stack = stack[:len(stack)-1]
				if len(stack) == 0 {
					return position + 1, nil
				}
			}
		}
		return 0, fmt.Errorf("JSON nested value is unterminated")
	}
	position := start
	for position < len(payload) && payload[position] != ',' && payload[position] != '}' &&
		payload[position] != ']' && !isJSONWhitespace(payload[position]) {
		position++
	}
	if position == start {
		return 0, fmt.Errorf("JSON primitive value is empty")
	}
	return position, nil
}

func matchingJSONDelimiter(value byte) byte {
	if value == '{' {
		return '}'
	}
	return ']'
}

func skipJSONWhitespace(payload []byte, position int) int {
	for position < len(payload) && isJSONWhitespace(payload[position]) {
		position++
	}
	return position
}

func isJSONWhitespace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n'
}

func findLegacyCheckpointImageIdentity(
	checkpoint *mediaCheckpoint,
	opts mediaResolutionOptions,
	entry mediaCheckpointEntry,
) (legacyCheckpointMediaIdentity, error) {
	current := findMediaRequirement(opts.Plan.RequiredMedia, entry.LogicalID)
	if current == nil || current.MediaType != "image" || current.URL != "" || current.LocalPath == "" {
		return legacyCheckpointMediaIdentity{}, fmt.Errorf("current checkpoint media requirement is not a local image")
	}
	candidates := make([]legacyCheckpointMediaIdentity, 0, 1)
	for _, bundleDir := range checkpoint.BundleDirs {
		if err := validateOwnedImportBundle(bundleDir, opts.BundleRoot); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return legacyCheckpointMediaIdentity{}, err
		}
		planPath := filepath.Join(bundleDir, "plan.json")
		plan, err := readLegacyCanvasPlanNoFollow(planPath)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return legacyCheckpointMediaIdentity{}, fmt.Errorf("read previous CanvasPlan: %w", err)
		}
		if plan.Source != checkpoint.Source {
			return legacyCheckpointMediaIdentity{}, fmt.Errorf("previous CanvasPlan source does not match the checkpoint")
		}
		previous := findMediaRequirement(plan.RequiredMedia, entry.LogicalID)
		if previous == nil || previous.SHA256 != entry.SHA256 {
			continue
		}
		if !sameLegacyMediaRequirement(*previous, *current) {
			return legacyCheckpointMediaIdentity{}, fmt.Errorf("previous checkpoint media requirement does not match the current source node and media contract")
		}
		previousPath := filepath.Join(bundleDir, filepath.FromSlash(previous.LocalPath))
		if err := requireFileWithinBundle(previousPath, bundleDir); err != nil {
			return legacyCheckpointMediaIdentity{}, fmt.Errorf("validate previous checkpoint image path: %w", err)
		}
		identity, err := inspectImportMediaFile(previousPath)
		if err != nil {
			return legacyCheckpointMediaIdentity{}, fmt.Errorf("inspect previous checkpoint image: %w", err)
		}
		if previous.Metadata.ByteSize == nil || identity.ByteSize != *previous.Metadata.ByteSize {
			return legacyCheckpointMediaIdentity{}, fmt.Errorf("previous checkpoint image byte size does not match its CanvasPlan")
		}
		if identity.RawSHA256 != entry.SHA256 {
			return legacyCheckpointMediaIdentity{}, fmt.Errorf("previous checkpoint image raw SHA-256 does not match the checkpoint")
		}
		candidates = append(candidates, legacyCheckpointMediaIdentity{
			ContentFingerprint: identity.ContentFingerprint,
			ByteSize:           identity.ByteSize,
		})
	}
	if len(candidates) == 0 {
		return legacyCheckpointMediaIdentity{}, fmt.Errorf("no retained export bundle contains the checkpointed raw image")
	}
	if len(candidates) != 1 {
		return legacyCheckpointMediaIdentity{}, fmt.Errorf("multiple retained export bundles ambiguously match the checkpointed raw image")
	}
	return candidates[0], nil
}

func readLegacyCanvasPlanNoFollow(path string) (canvasplan.Plan, error) {
	file, err := openImportMediaNoFollow(path)
	if err != nil {
		return canvasplan.Plan{}, err
	}
	defer file.Close()
	initial, err := file.Stat()
	if err != nil {
		return canvasplan.Plan{}, err
	}
	plan, decodeErr := canvasplan.DecodePlan(file)
	stableErr := validateStableImportMediaFile(path, file, initial)
	if decodeErr != nil {
		return canvasplan.Plan{}, decodeErr
	}
	if stableErr != nil {
		return canvasplan.Plan{}, stableErr
	}
	return plan, nil
}

func findMediaRequirement(requirements []canvasplan.MediaRequirement, logicalID string) *canvasplan.MediaRequirement {
	for index := range requirements {
		if requirements[index].LogicalID == logicalID {
			return &requirements[index]
		}
	}
	return nil
}

func sameLegacyMediaRequirement(previous, current canvasplan.MediaRequirement) bool {
	return previous.LogicalID == current.LogicalID &&
		previous.SourceNodeID == current.SourceNodeID &&
		previous.FileName == current.FileName &&
		previous.MediaType == current.MediaType &&
		previous.URL == current.URL &&
		previous.LocalPath == current.LocalPath &&
		sameMediaMetadata(previous.Metadata, current.Metadata)
}

func sameMediaMetadata(previous, current canvasplan.MediaMetadata) bool {
	return optionalInt64Equal(previous.ByteSize, current.ByteSize) &&
		optionalInt64Equal(previous.DurationMS, current.DurationMS) &&
		previous.Extension == current.Extension &&
		optionalInt64Equal(previous.Height, current.Height) &&
		previous.MimeType == current.MimeType &&
		optionalInt64Equal(previous.Width, current.Width)
}

func optionalInt64Equal(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func validateOwnedImportBundle(bundleDir, bundleRoot string) error {
	root, err := filepath.Abs(bundleRoot)
	if err != nil {
		return fmt.Errorf("resolve canvas import bundle root: %w", err)
	}
	bundle, err := filepath.Abs(bundleDir)
	if err != nil {
		return fmt.Errorf("resolve checkpoint bundle: %w", err)
	}
	relative, err := filepath.Rel(root, bundle)
	if err != nil || strings.Contains(relative, string(filepath.Separator)) || !strings.HasPrefix(relative, "export-") {
		return fmt.Errorf("checkpoint bundle is outside the owned import bundle root")
	}
	info, err := os.Lstat(bundle)
	if err != nil {
		return fmt.Errorf("inspect checkpoint bundle: %w", err)
	}
	if fileInfoIsImportMediaLinkLike(info) || !info.IsDir() {
		return fmt.Errorf("checkpoint bundle must be a real directory")
	}
	return nil
}

func validImportMediaContentFingerprint(value string) bool {
	for _, prefix := range []string{libTVPNGAIGCFingerprintPrefix, rawMediaFingerprintPrefix} {
		if strings.HasPrefix(value, prefix) {
			digest := strings.TrimPrefix(value, prefix)
			if len(digest) != sha256.Size*2 || strings.ToLower(digest) != digest {
				return false
			}
			_, err := hex.DecodeString(digest)
			return err == nil
		}
	}
	return false
}
