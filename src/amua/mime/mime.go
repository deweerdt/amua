package mime

import (
	"bytes"
	"encoding/base64"
	"io"
	"io/ioutil"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"strings"
)

type ParserContext struct {
	Ctx interface{}
	Err error
}

type ParseFn func(*ParserContext, io.Reader, string, map[string]string)

func WalkParts(r io.Reader, parse ParseFn, pc *ParserContext, max_depth int) error {
	msg, err := mail.ReadMessage(r)
	if err != nil {
		return err
	}
	return PartWalker(msg.Body, msg.Header, parse, pc, max_depth)
}

func get_header(i map[string][]string, header string) string {
	h, ok := textproto.MIMEHeader(i)[textproto.CanonicalMIMEHeaderKey(header)]
	if ok {
		return h[0]
	}
	return ""
}

func PartWalker(r io.Reader, header map[string][]string, parse ParseFn, pc *ParserContext, depth int) error {
	depth--
	if depth < 0 {
		return nil
	}
	content_type := get_header(header, "content-type")
	media_type, params, err := mime.ParseMediaType(content_type)
	if err != nil {
		media_type = "text/plain"
	}

	is_multipart := true
	boundary := ""
	media_type = strings.ToLower(media_type)
	if !strings.HasPrefix(media_type, "multipart/") {
		is_multipart = false
	} else {
		var ok bool
		boundary, ok = params["boundary"]
		if !ok {
			is_multipart = false
		}
	}

	if is_multipart {
		mr := multipart.NewReader(r, boundary)
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			err = PartWalker(p, p.Header, parse, pc, depth)
			if err != nil {
				return err
			}
		}
		return nil
	}

	qp := false
	cte := strings.ToLower(get_header(header, "Content-Transfer-Encoding"))

	buf, err := ioutil.ReadAll(r)
	if err != nil {
		return err
	}
	br := bytes.NewReader(buf)

	var reader io.Reader
	switch cte {
	case "base64":
		reader = base64.NewDecoder(base64.StdEncoding, br)
	case "quoted-printable":
		qp = true
		reader = quotedprintable.NewReader(br)
	default:
		reader = br
	}
retry:
	decoded_buf, err := ioutil.ReadAll(reader)
	if err != nil {
		panic("ok")
		if qp {
			/* qp tends to fail often, retry in non-qp */
			qp = false
			br.Seek(0, 0)
			reader = br
			goto retry
		}
		return err
	}
	parse(pc, bytes.NewBuffer(decoded_buf), media_type, params)
	return nil
}
