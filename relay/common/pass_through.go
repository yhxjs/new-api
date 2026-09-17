package common

import (
	"bufio"
	"bytes"
	"io"
	"mime"
	"net/textproto"
	"net/url"
	"strings"

	hostcommon "github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// NewPassThroughRequestBody returns the original request body unless a mapped
// model needs to be sent upstream. The original storage remains unchanged
// so a retry can apply a different channel mapping.
func NewPassThroughRequestBody(c *gin.Context, modelName string, modelMapped bool) (hostcommon.ReplayableBody, io.Closer, error) {
	storage, err := hostcommon.GetBodyStorage(c)
	if err != nil {
		return nil, nil, err
	}
	if !modelMapped {
		return hostcommon.NewReplayableBodyReader(storage), nil, nil
	}
	mediaType, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	isJSON := mediaType == "application/json" || strings.HasSuffix(mediaType, "+json")
	if err != nil || (!isJSON && mediaType != "application/x-www-form-urlencoded" && mediaType != "multipart/form-data") {
		return hostcommon.NewReplayableBodyReader(storage), nil, nil
	}

	raw, err := storage.Bytes()
	if err != nil {
		return nil, nil, err
	}
	var patched []byte
	switch {
	case isJSON:
		model := gjson.GetBytes(raw, "model")
		if !model.Exists() || model.Type != gjson.String || model.String() == modelName {
			return hostcommon.NewReplayableBodyReader(storage), nil, nil
		}
		patched, err = sjson.SetBytes(raw, "model", modelName)
	case mediaType == "application/x-www-form-urlencoded":
		fields := bytes.Split(raw, []byte("&"))
		for i, field := range fields {
			key, value, _ := bytes.Cut(field, []byte("="))
			name, keyErr := url.QueryUnescape(string(key))
			if keyErr != nil || name != "model" {
				continue
			}
			current, valueErr := url.QueryUnescape(string(value))
			if valueErr != nil || current != modelName {
				fields[i] = []byte(string(key) + "=" + url.QueryEscape(modelName))
			}
		}
		patched = bytes.Join(fields, []byte("&"))
	case mediaType == "multipart/form-data":
		patched, err = rewriteMultipartModel(raw, params["boundary"], modelName)
	}
	if err != nil {
		return nil, nil, err
	}
	if bytes.Equal(raw, patched) {
		return hostcommon.NewReplayableBodyReader(storage), nil, nil
	}
	upstreamStorage, err := hostcommon.CreateBodyStorage(patched)
	if err != nil {
		return nil, nil, err
	}
	return hostcommon.NewReplayableBodyReader(upstreamStorage), upstreamStorage, nil
}

// Patch only field payloads: re-encoding multipart with a Writer would also
// change boundaries, header formatting, part order, and possibly file bytes.
func rewriteMultipartModel(raw []byte, boundary, modelName string) ([]byte, error) {
	if boundary == "" {
		return nil, io.ErrUnexpectedEOF
	}
	marker := []byte("--" + boundary)
	partStart, copied, scan := -1, 0, 0
	var patched []byte
	var newline []byte
	for {
		index := bytes.Index(raw[scan:], marker)
		if index < 0 {
			return nil, io.ErrUnexpectedEOF
		}
		index += scan
		scan = index + len(marker)
		if index > 0 && raw[index-1] != '\n' {
			continue
		}
		if partStart >= 0 && !bytes.HasSuffix(raw[:index], newline) {
			continue
		}
		end := scan
		final := bytes.HasPrefix(raw[end:], []byte("--"))
		if final {
			end += 2
		}
		for end < len(raw) && (raw[end] == ' ' || raw[end] == '\t') {
			end++
		}
		lineEnd := end
		switch {
		case end == len(raw) && final:
		case bytes.HasPrefix(raw[end:], []byte("\r\n")):
			end += 2
		case end < len(raw) && raw[end] == '\n':
			end++
		default:
			continue
		}
		if newline == nil {
			newline = raw[lineEnd:end]
		} else if end > lineEnd && !bytes.Equal(raw[lineEnd:end], newline) {
			continue
		}
		if partStart >= 0 {
			partEnd := index - len(newline)
			if partEnd < partStart {
				return nil, io.ErrUnexpectedEOF
			}
			part := bytes.NewReader(raw[partStart:partEnd])
			buffered := bufio.NewReader(part)
			headers, err := textproto.NewReader(buffered).ReadMIMEHeader()
			if err != nil {
				return nil, err
			}
			disposition, params, _ := mime.ParseMediaType(headers.Get("Content-Disposition"))
			if disposition == "form-data" && params["name"] == "model" && params["filename"] == "" {
				valueStart := partEnd - part.Len() - buffered.Buffered()
				if string(raw[valueStart:partEnd]) != modelName {
					patched = append(patched, raw[copied:valueStart]...)
					patched = append(patched, modelName...)
					copied = partEnd
				}
			}
		}
		if final {
			if patched == nil {
				return raw, nil
			}
			return append(patched, raw[copied:]...), nil
		}
		partStart = end
		scan = partStart
	}
}
