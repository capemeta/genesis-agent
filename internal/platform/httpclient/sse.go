package httpclient

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"
)

type sseStream struct {
	reader *bufio.Reader
	body   io.ReadCloser
	cancel func()
}

func newSSEStream(body io.ReadCloser, cancel func()) EventStream {
	return &sseStream{
		reader: bufio.NewReader(body),
		body:   body,
		cancel: cancel,
	}
}

func (s *sseStream) Recv() (*SSEEvent, error) {
	var event SSEEvent
	var dataLines []string

	for {
		line, err := s.reader.ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, &Error{Kind: ErrorKindSSE, Message: "读取 SSE 数据失败", Err: err}
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if len(dataLines) == 0 && event.Event == "" && event.ID == "" && event.Retry == 0 {
				if errors.Is(err, io.EOF) {
					return nil, io.EOF
				}
				continue
			}
			event.Data = []byte(strings.Join(dataLines, "\n"))
			return &event, nil
		}

		if strings.HasPrefix(line, ":") {
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			continue
		}

		field, value, found := strings.Cut(line, ":")
		if !found {
			field = line
			value = ""
		} else {
			value = strings.TrimPrefix(value, " ")
		}

		switch field {
		case "event":
			event.Event = value
		case "data":
			dataLines = append(dataLines, value)
		case "id":
			event.ID = value
		case "retry":
			if retry, convErr := strconv.Atoi(value); convErr == nil {
				event.Retry = retry
			}
		}

		if errors.Is(err, io.EOF) {
			event.Data = bytes.Join(stringSliceToBytes(dataLines), []byte("\n"))
			if len(event.Data) == 0 && event.Event == "" && event.ID == "" && event.Retry == 0 {
				return nil, io.EOF
			}
			return &event, nil
		}
	}
}

func (s *sseStream) Close() error {
	if s.cancel != nil {
		s.cancel()
	}
	if s.body != nil {
		return s.body.Close()
	}
	return nil
}

func stringSliceToBytes(values []string) [][]byte {
	result := make([][]byte, 0, len(values))
	for _, value := range values {
		result = append(result, []byte(value))
	}
	return result
}
