package provider

import (
	"bufio"
	"io"
	"strings"
)

type sseEvent struct {
	Event string
	Data  string
}

type sseReader struct {
	sc *bufio.Scanner
}

func newSSEReader(r io.Reader) *sseReader {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	return &sseReader{sc: sc}
}

func (r *sseReader) Next() (sseEvent, bool, error) {
	var ev sseEvent
	var data strings.Builder
	got := false

	flush := func() (sseEvent, bool) {
		if !got {
			return sseEvent{}, false
		}
		ev.Data = strings.TrimRight(data.String(), "\n")
		return ev, true
	}

	for r.sc.Scan() {
		line := r.sc.Text()
		if line == "" {
			if e, ok := flush(); ok {
				return e, true, nil
			}
			continue
		}
		got = true
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			ev.Event = value
		case "data":
			data.WriteString(value)
			data.WriteByte('\n')
		}
	}
	if err := r.sc.Err(); err != nil {
		return sseEvent{}, false, err
	}
	if e, ok := flush(); ok {
		return e, true, nil
	}
	return sseEvent{}, false, nil
}
