package config

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

type Config struct {
	Maildirs []string
}

func highlightBytePosition(f io.Reader, pos int64) (line, col int, highlight string) {
	line = 1
	br := bufio.NewReader(f)
	lastLine := ""
	thisLine := new(bytes.Buffer)
	for n := int64(0); n < pos; n++ {
		b, err := br.ReadByte()
		if err != nil {
			break
		}
		if b == '\n' {
			lastLine = thisLine.String()
			thisLine.Reset()
			line++
			col = 1
		} else {
			col++
			thisLine.WriteByte(b)
		}
	}
	if line > 1 {
		highlight += fmt.Sprintf("%5d: %s\n", line-1, lastLine)
	}
	highlight += fmt.Sprintf("%5d: %s\n", line, thisLine.String())
	highlight += fmt.Sprintf("%s^\n", strings.Repeat(" ", col+5))
	return
}

func NewConfig(filename string) (*Config, error) {
	cfg := &Config{}

	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	dec := json.NewDecoder(bufio.NewReader(file))
	for {
		if err := dec.Decode(&cfg); err == io.EOF {
			err = nil
			break
		} else if err != nil {
			extra := ""
			if serr, ok := err.(*json.SyntaxError); ok {
				if _, serr := file.Seek(0, os.SEEK_SET); serr != nil {
					return nil, fmt.Errorf("seek error: %s", serr)
				}
				line, col, highlight := highlightBytePosition(file, serr.Offset)
				extra = fmt.Sprintf(":\nError at line %s, column %s (file offset %s):\n%s", line, col, serr.Offset, highlight)
			}
			return nil, fmt.Errorf("error parsing JSON object in config file %s%s\n%s", file.Name(), extra, err)

		}
	}

	return cfg, nil
}
