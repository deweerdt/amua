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

type MimePart struct {
	MimeType           MimeType
	ContentDisposition ContentDisposition
	Name               string
	Next, Prev         *MimePart
	Child, Parent      *MimePart
	Buf                *bytes.Buffer
}

type MimeTreeBuilder struct {
	root      *MimePart
	cur       *MimePart
	curParent *MimePart
	prevPath  []int
}

func NewMimeType(mt MimeTypeInt) MimeType {
	return MimeType{mt, ""}
}

func NewMimeTypeOther(s string) MimeType {
	return MimeType{MimeTypeOther, s}
}

func buildMimeTree(pc *ParserContext, path []int, r io.Reader, pd PartDescr) error {
	if pc.Err != nil {
		return pc.Err
	}
	var mtb *MimeTreeBuilder
	mtb = pc.Ctx.(*MimeTreeBuilder)
	mp := MimePart{}
	name, ok := pd.CDParams["filename"]
	if ok {
		mp.Name = name
	}
	mp.ContentDisposition = pd.ContentDisposition
	if mtb.root == nil {
		mtb.root = &mp
	}
	if r != nil {
		buf, err := ioutil.ReadAll(r)
		if err != nil {
			pc.Err = err
			return err
		}
		mp.Buf = bytes.NewBuffer(buf)
	}
	prev := mtb.cur
	switch pd.MediaType {
	case "text/plain":
		mp.MimeType = NewMimeType(TextPlain)
	case "text/html":
		mp.MimeType = NewMimeType(TextHtml)
	case "multipart/mixed":
		mp.MimeType = NewMimeType(MultipartMixed)
	case "multipart/alternative":
		mp.MimeType = NewMimeType(MultipartAlternative)
	case "multipart/digest":
		mp.MimeType = NewMimeType(MultipartDigest)
	case "multipart/parallel":
		mp.MimeType = NewMimeType(MultipartParallel)
	case "multipart/related":
		mp.MimeType = NewMimeType(MultipartRelated)
	default:
		mp.MimeType = NewMimeTypeOther(pd.MediaType)
	}
	switch {
	case len(path) == len(mtb.prevPath):
		mp.Prev = prev
		if prev != nil {
			mp.Prev.Next = &mp
			mp.Parent = prev.Parent
		}
	case len(path) < len(mtb.prevPath):
		mp.Prev = prev.Parent
		if mp.Prev != nil {
			mp.Prev.Next = &mp
		}
	case len(path) > len(mtb.prevPath):
		mp.Parent = prev
		if prev != nil {
			mp.Parent.Child = &mp
		}
	}
	mtb.cur = &mp
	mtb.prevPath = path
	return nil
}

func GetMimeTree(r io.Reader, limit int) (*MimePart, error) {
	pc := ParserContext{}
	mtb := &MimeTreeBuilder{}
	pc.Ctx = mtb
	err := WalkParts(r, buildMimeTree, &pc, limit)
	if err != nil {
		return nil, err
	}
	return mtb.root, nil
}

type ParserContext struct {
	Ctx interface{}
	Err error
}

func ContentDispositionFromStr(s string) ContentDisposition {
	switch s {
	case "":
		fallthrough
	case "inline":
		return CDInline
	default:
		return CDAttachment
	}
}

type MimeType struct {
	MimeTypeInt MimeTypeInt
	Other       string
}

func (mt *MimeType) IsMultipart() bool {
	switch mt.MimeTypeInt {
	case MultipartMixed:
		fallthrough
	case MultipartAlternative:
		fallthrough
	case MultipartDigest:
		fallthrough
	case MultipartParallel:
		fallthrough
	case MultipartRelated:
		return true
	}
	return false
}
func (mt *MimeType) Is(mti MimeTypeInt) bool {
	return mt.MimeTypeInt == mti
}

type MimeTypeInt uint

const (
	TextPlain MimeTypeInt = iota
	TextHtml
	// https://tools.ietf.org/html/rfc2046
	MultipartMixed
	MultipartAlternative
	MultipartDigest
	MultipartParallel
	// https://tools.ietf.org/html/rfc2387
	MultipartRelated
	MimeTypeOther
)

var mimeTypeTxt = map[MimeTypeInt]string{
	TextPlain:            "text/plain",
	TextHtml:             "text/html",
	MultipartMixed:       "multipart/mixed",
	MultipartAlternative: "multipart/alternative",
	MultipartDigest:      "multipart/digest",
	MultipartParallel:    "multipart/parallel",
	MultipartRelated:     "multipart/related",
}

func MimeTypeTxt(mt MimeType) string {
	s, ok := mimeTypeTxt[mt.MimeTypeInt]
	if ok {
		return s
	}
	return mt.Other
}

type ContentDisposition uint

const (
	CDInline ContentDisposition = iota
	CDAttachment
)

type PartDescr struct {
	MediaType          string
	Params             map[string]string
	ContentDisposition ContentDisposition
	CDParams           map[string]string
}

type ParseFn func(*ParserContext, []int, io.Reader, PartDescr) error

func WalkParts(r io.Reader, parse ParseFn, pc *ParserContext, maxDepth int) error {
	msg, err := mail.ReadMessage(r)
	if err != nil {
		return err
	}
	return partWalker(msg.Body, []int{}, msg.Header, parse, pc, maxDepth)
}

func getHeader(i map[string][]string, header string) string {
	h, ok := textproto.MIMEHeader(i)[textproto.CanonicalMIMEHeaderKey(header)]
	if ok {
		return h[0]
	}
	return ""
}

func partWalker(r io.Reader, path []int, header map[string][]string, parse ParseFn, pc *ParserContext, depth int) error {
	depth--
	if depth < 0 {
		return nil
	}
	cds := getHeader(header, "content-disposition")
	contentDisposition_str, cdParams, err := mime.ParseMediaType(cds)
	var contentDisposition ContentDisposition
	if err != nil {
		contentDisposition = CDInline
	} else {
		contentDisposition = ContentDispositionFromStr(contentDisposition_str)
	}

	contentType := getHeader(header, "content-type")
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = "text/plain"
	}

	isMultipart := true
	boundary := ""
	mediaType = strings.ToLower(mediaType)
	if !strings.HasPrefix(mediaType, "multipart/") {
		isMultipart = false
	} else {
		var ok bool
		boundary, ok = params["boundary"]
		if !ok {
			isMultipart = false
		}
	}

	partIndex := 0
	if isMultipart {
		err = parse(pc, path, nil, PartDescr{mediaType, params, contentDisposition, cdParams})
		if err != nil {
			return err
		}
		mr := multipart.NewReader(r, boundary)
		for {
			p, err := mr.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			err = partWalker(p, append(path, partIndex), p.Header, parse, pc, depth)
			if err != nil {
				return err
			}
			partIndex++
		}
		return nil
	}

	qp := false
	cte := strings.ToLower(getHeader(header, "Content-Transfer-Encoding"))

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
	decodedBuf, err := ioutil.ReadAll(reader)
	if err != nil {
		if qp {
			/* qp tends to fail often, retry in non-qp */
			qp = false
			br.Seek(0, 0)
			reader = br
			goto retry
		}
		return err
	}
	return parse(pc, path, bytes.NewBuffer(decodedBuf), PartDescr{mediaType, params, contentDisposition, cdParams})
}
