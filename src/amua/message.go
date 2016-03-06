package main

import (
	gomime "mime"
	"time"
	"fmt"
	"io"
	"os"
	"strings"
	"bytes"
	"net/mail"

	"amua/mime"
	"amua/util"

	"github.com/deweerdt/gocui"
	"github.com/jaytaylor/html2text"
	"github.com/mitchellh/colorstring"
)

type ByDate []*Message

func (a ByDate) Len() int           { return len(a) }
func (a ByDate) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByDate) Less(i, j int) bool { return a[i].Date.After(a[j].Date) }

type Message struct {
	From    string
	To      string
	Subject string
	CCs     string
	ReplyTo string
	Date    time.Time
	path    string
	rs      *readState
	size    int64
	Flags   MessageFlags
}


type MessageFlags uint

const (
	Passed  MessageFlags = 1 << iota //Flag "P" (passed): the user has resent/forwarded/bounced this message to someone else.
	Replied                          //Flag "R" (replied): the user has replied to this message.
	Seen                             //Flag "S" (seen): the user has viewed this message, though perhaps he didn't read all the way through it.
	Trashed                          //Flag "T" (trashed): the user has moved this message to the trash; the trash will be emptied by a later user action.
	Draft                            //Flag "D" (draft): the user considers this message a draft; toggled at user discretion.
	Flagged                          //Flag "F" (flagged): user-defined flag; toggled at user discretion.
	/* Those are internal, and aren't saved to disk */
	Tagged //Tagged, used for tagged actions

)

func flagsToString(f MessageFlags) string {
	ret := make([]byte, 4)
	if (f & Seen) == 0 {
		ret[0] = 'N'
	} else if (f & Replied) != 0 {
		ret[0] = 'r'
	} else if (f & Passed) != 0 {
		ret[0] = 'p'
	}
	if (f & Trashed) != 0 {
		ret[1] = 'D'
	}
	if (f & Draft) != 0 {
		ret[2] = 'd'
	}
	if (f & Flagged) != 0 {
		ret[3] = '!'
	}
	return string(ret)
}
func parseFlags(s string) MessageFlags {
	var ret MessageFlags
	for _, c := range s {
		switch c {
		case 'P':
			ret |= Passed
		case 'R':
			ret |= Replied
		case 'S':
			ret |= Seen
		case 'T':
			ret |= Trashed
		case 'D':
			ret |= Draft
		case 'F':
			ret |= Flagged
		}
	}
	return ret
}

func flagsToFile(f MessageFlags) string {
	ret := ""
	if (f & Passed) != 0 {
		ret += "P"
	}
	if (f & Replied) != 0 {
		ret += "R"
	}
	if (f & Seen) != 0 {
		ret += "S"
	}
	if (f & Trashed) != 0 {
		ret += "T"
	}
	if (f & Draft) != 0 {
		ret += "D"
	}
	if (f & Flagged) != 0 {
		ret += "F"
	}
	return ret
}

var isMe func(m *mail.Address) bool

func buildCCs(m *Message) []*mail.Address {
	cc, err := mail.ParseAddressList(m.CCs)
	if err != nil {
		cc = []*mail.Address{}
	}
	to, err := mail.ParseAddressList(m.To)
	if err != nil {
		to = []*mail.Address{}
	}
	all := append(to, cc...)
	ret := make([]*mail.Address, 0)
	for _, e := range all {
		if !isMe(e) {
			ret = append(ret, e)
		}
	}
	return ret
}
func buildTo(m *Message) []*mail.Address {
	if m.ReplyTo != "" {
		ret, err := mail.ParseAddress(m.ReplyTo)
		if err == nil {
			return []*mail.Address{ret}
		}
	}
	if m.From != "" {
		ret, err := mail.ParseAddress(m.From)
		if err == nil {
			return []*mail.Address{ret}
		}
	}

	return []*mail.Address{}
}
func dehtmlize(in *bytes.Buffer) *bytes.Buffer {
	out, err := html2text.FromReader(in)
	if err != nil {
		ret := &bytes.Buffer{}
		ret.WriteString(err.Error())
		ret.WriteString("\n")
		if false {
			ret.Write(in.Bytes())
		}
		return ret
	}
	ret := bytes.NewBufferString(out)
	return ret
}

func partSummary(m *mime.MimePart) *bytes.Buffer {
	name := ""
	if m.Name != "" {
		name = fmt.Sprintf("- %s ", m.Name)
	}

	str := fmt.Sprintf("\n\033[7m[-- %s %s- (%s) --]\n", mime.MimeTypeTxt(m.MimeType), name, util.SiteToHuman(int64(m.Buf.Len())))
	return bytes.NewBufferString(str)
}

func traverse(m *mime.MimePart, showParts bool) []*bytes.Buffer {
	ret := make([]*bytes.Buffer, 0)
	if m.MimeType.IsMultipart() && m.Child == nil {
		return ret
	}
	switch m.MimeType.MimeTypeInt {
	case mime.MultipartDigest:
		/* not implemented yet */
		fallthrough
	case mime.MultipartParallel:
		fallthrough
	case mime.MultipartRelated:
		fallthrough
	case mime.MultipartMixed:
		ret = append(ret, traverse(m.Child, showParts)...)
	case mime.MultipartAlternative:
		var plain *mime.MimePart
		var html *mime.MimePart
		var last *mime.MimePart
		for cur := m.Child; cur != nil; cur = cur.Next {
			if cur.MimeType.Is(mime.TextPlain) {
				plain = cur
			} else if cur.MimeType.Is(mime.TextHtml) {
				html = cur
			}
			last = cur
		}
		if plain != nil {
			if plain.ContentDisposition == mime.CDInline {
				ret = append(ret, plain.Buf)
			} else {
				ret = append(ret, partSummary(plain))
			}
		} else if html != nil {
			if html.ContentDisposition == mime.CDInline {
				ret = append(ret, dehtmlize(html.Buf))
			} else {
				ret = append(ret, partSummary(html))
			}
		} else if last != nil {
			if last.MimeType.IsMultipart() {
				ret = append(ret, traverse(last, showParts)...)
			} else {
				ret = append(ret, partSummary(last))
			}
		}
	case mime.TextPlain:
		if m.ContentDisposition == mime.CDInline {
			ret = append(ret, m.Buf)
		} else {
			ret = append(ret, partSummary(m))
		}
	case mime.TextHtml:
		if m.ContentDisposition == mime.CDInline {
			ret = append(ret, dehtmlize(m.Buf))
		} else {
			ret = append(ret, partSummary(m))
		}
	default:
		ret = append(ret, partSummary(m))

	}
	if m.Next != nil {
		ret = append(ret, traverse(m.Next, showParts)...)
	}
	return ret
}

type MessageAsMimeTree Message
type MessageAsText Message

func (m *MessageAsMimeTree) Draw(amua *Amua, g *gocui.Gui) error {
	v, err := g.View(MESSAGE_VIEW)
	if err != nil {
		return err
	}
	v.Clear()
	v.Wrap = true
	v.SetOrigin(0, 0)

	colorstring.Fprintf(v, "[green]Subject: %s\n", m.Subject)
	colorstring.Fprintf(v, "[red]From: %s\n", m.From)
	colorstring.Fprintf(v, "[red]To: %s\n", m.To)
	colorstring.Fprintf(v, "[green]Date: %s\n", m.Date.Format("Mon, 2 Jan 2006 15:04:05 -0700"))
	fmt.Fprintf(v, "\n")
	_, err = io.Copy(v, m)
	if err != nil {
		return err
	}
	return nil

}

func (m *MessageAsMimeTree) Read(p []byte) (int, error) {
	if m.rs == nil {
		m.rs = &readState{}
		var printM func(w io.Writer, depth int, m *mime.MimePart)
		printM = func(w io.Writer, depth int, m *mime.MimePart) {
			fmt.Fprintf(w, "%s%s\n", strings.Repeat("-", depth), mime.MimeTypeTxt(m.MimeType))
			if m.Child != nil {
				printM(w, depth+1, m.Child)
			}
			for cur := m.Next; cur != nil; cur = cur.Next {
				printM(w, depth, cur)
			}
		}
		f, err := os.Open(m.path)
		if err != nil {
			m.rs = nil
			return 0, err
		}
		defer f.Close()
		mtree, err := mime.GetMimeTree(f, 10)
		if err != nil {
			m.rs = nil
			return 0, err
		}
		buf := &bytes.Buffer{}
		printM(buf, 0, mtree)
		m.rs.r = buf

	}
	ret, err := m.rs.r.Read(p)
	if err != nil {
		m.rs = nil
	}
	return ret, err
}

func (m *Message) Read(p []byte) (int, error) {
	var err error
	if m.rs == nil {
		m.rs = &readState{}
		f, err := os.Open(m.path)
		if err != nil {
			m.rs = nil
			return 0, err
		}
		defer f.Close()
		mtree, err := mime.GetMimeTree(f, 10)
		if err != nil {
			m.rs = nil
			return 0, err
		}

		m.rs.buffers = traverse(mtree, true)

		readers := make([]io.Reader, len(m.rs.buffers))
		for i, b := range m.rs.buffers {
			newb := bytes.Replace(b.Bytes(), []byte("\r\n"), []byte("\n"), -1)
			readers[i] = bytes.NewBuffer(newb)
		}
		m.rs.r = io.MultiReader(readers...)
	}
	ret, err := m.rs.r.Read(p)
	if err != nil {
		m.rs = nil
	}
	return ret, err
}

func (m *MessageAsText) Draw(amua *Amua, g *gocui.Gui) error {
	v, err := g.View(MESSAGE_VIEW)
	if err != nil {
		return err
	}
	v.Clear()
	v.Wrap = true
	v.SetOrigin(0, 0)

	_, err = io.Copy(v, m)
	if err != nil {
		return err
	}
	return nil

}

func (m *MessageAsText) Read(p []byte) (int, error) {
	var err error
	if m.rs == nil {
		m.rs = &readState{}
		f, err := os.Open(m.path)
		if err != nil {
			m.rs = nil
			return 0, err
		}
		defer f.Close()
		mtree, err := mime.GetMimeTree(f, 10)
		if err != nil {
			m.rs = nil
			return 0, err
		}

		m.rs.buffers = traverse(mtree, true)

		readers := make([]io.Reader, len(m.rs.buffers))
		for i, b := range m.rs.buffers {
			newb := bytes.Replace(b.Bytes(), []byte("\r\n"), []byte("\n"), -1)
			readers[i] = bytes.NewBuffer(newb)
		}
		m.rs.r = io.MultiReader(readers...)
	}
	ret, err := m.rs.r.Read(p)
	if err != nil {
		m.rs = nil
	}
	return ret, err
}

var dec = new(gomime.WordDecoder)

func mimedec(hdr string) string {
	dhdr, err := dec.DecodeHeader(hdr)
	if err != nil {
		dhdr = hdr
	}
	return dhdr
}

func LoadMessage(path string) (*Message, error) {
	m := &Message{path: path}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	msg, err := mail.ReadMessage(f)
	if err != nil {
		return nil, err
	}
	m.From = mimedec(msg.Header.Get("From"))
	m.ReplyTo = mimedec(msg.Header.Get("reply-to"))
	m.CCs = mimedec(msg.Header.Get("cc"))
	m.To = mimedec(msg.Header.Get("To"))

	m.Subject = mimedec(msg.Header.Get("Subject"))
	m.Date, _ = msg.Header.Date()
	m.size = fi.Size()

	i := strings.LastIndex(path, ":2,")
	if i != -1 {
		m.Flags = parseFlags(path[i+3:])
	}
	return m, nil
}

