package main

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"amua/config"

	"github.com/deweerdt/gocui"
	"github.com/mitchellh/colorstring"
)

type ByDate []*Message

func (a ByDate) Len() int           { return len(a) }
func (a ByDate) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByDate) Less(i, j int) bool { return a[i].Date.After(a[j].Date) }

type Maildir struct {
	path     string
	messages []*Message
}

type read_state struct {
	r       io.Reader
	done    func()
	buffers []*bytes.Buffer
}

type Message struct {
	From    string
	To      string
	Subject string
	Date    time.Time
	path    string
	rs      *read_state
	size    int64
}

type ParserContext struct {
	ctx interface{}
	err error
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
	} else {
		return ""
	}
}

func PartWalker(r io.Reader, header map[string][]string, parse ParseFn, pc *ParserContext, depth int) error {
	depth--
	if depth < 0 {
		return nil
	}
	content_type := get_header(header, "content-type")
	media_type, params, err := mime.ParseMediaType(content_type)
	if err != nil {
		panic(0)
		return err
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

func to_text(pc *ParserContext, r io.Reader, media_type string, params map[string]string) {
	if pc.err != nil {
		return
	}
	var rs *read_state
	rs = pc.ctx.(*read_state)
	if media_type == "text/plain" {
		buf, err := ioutil.ReadAll(r)
		if err != nil {
			pc.err = err
			return
		}
		rs.buffers = append(rs.buffers, bytes.NewBuffer(buf))
	}
}

func (m *Message) Read(p []byte) (int, error) {
	var err error
	if m.rs == nil {
		m.rs = &read_state{}
		f, err := os.Open(m.path)
		if err != nil {
			return 0, err
		}
		m.rs.buffers = make([]*bytes.Buffer, 0)
		m.rs.done = func() {
			f.Close()
		}
		pc := ParserContext{}
		pc.ctx = m.rs
		err = WalkParts(f, to_text, &pc, 10)
		if err != nil {
			return 0, err
		}
		readers := make([]io.Reader, len(m.rs.buffers))
		for i, b := range m.rs.buffers {
			readers[i] = b
		}
		m.rs.r = io.MultiReader(readers...)
	}
	ret, err := m.rs.r.Read(p)
	if err == io.EOF {
		m.rs.done()
		m.rs = nil
	}
	return ret, err
}

var dec = new(mime.WordDecoder)

func LoadMessage(path string) (*Message, error) {
	mimedec := func(hdr string) string {
		dhdr, err := dec.DecodeHeader(hdr)
		if err != nil {
			dhdr = hdr
		}
		return dhdr
	}
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
	m.To = mimedec(msg.Header.Get("To"))

	m.Subject = mimedec(msg.Header.Get("Subject"))
	m.Date, _ = msg.Header.Date()
	m.size = fi.Size()

	return m, nil
}

func LoadMaildir(md_path string) (*Maildir, error) {
	md := &Maildir{}
	fis, err := ioutil.ReadDir(md_path)
	if err != nil {
		return nil, err
	}
	msgs := make([]*Message, 0)
	for _, fi := range fis {
		m, err := LoadMessage(filepath.Join(md_path, fi.Name()))
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	md.messages = msgs
	return md, nil
}

func (md *Maildir) SortByDate() {
	sort.Sort(ByDate(md.messages))
}

type Mode int

const (
	MaildirMode Mode = 0
	MessageMode      = 1
	MaxMode          = 2
)

type View struct {
	mode             Mode
	cur_maildir_view *MaildirView
	cur_message_view *MessageView
}

func quit(g *gocui.Gui, v *gocui.View) error {
	return gocui.ErrQuit
}

const MAILDIR_VIEW = "maildir"
const MESSAGE_VIEW = "message"
const SLIDER_VIEW = "slider"
const SIDE_VIEW = "side"
const STATUS_VIEW = "status"

type MessageView struct {
	cur int
	msg *Message
}

func refresh_message(view *View, g *gocui.Gui) error {
	v, err := g.View(MESSAGE_VIEW)
	if err != nil {
		return err
	}
	v.Clear()
	v.SetOrigin(0, 0)

	m := view.cur_maildir_view.md.messages[view.cur_maildir_view.cur]
	colorstring.Fprintf(v, "[blue]Subject: %s\n", m.Subject)
	colorstring.Fprintf(v, "[red]From: %s\n", m.From)
	colorstring.Fprintf(v, "[red]To: %s\n", m.To)
	colorstring.Fprintf(v, "[blue]Date: %s\n", m.Date.Format("Mon, 2 Jan 2006 15:04:05 -0700"))
	fmt.Fprintf(v, "\n")
	_, err = io.Copy(v, m)
	if err != nil {
		return err
	}
	return nil

}

func scrollView(dy int) func(g *gocui.Gui, v *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		ox, oy := v.Origin()
		/* ignore errors */
		v.SetOrigin(ox, oy+dy)
		return nil
	}
}

func keybindings(view *View, g *gocui.Gui) error {
	switch_to_mode := func(mode Mode) func(g *gocui.Gui, v *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			view.mode = mode
			curview := ""
			switch view.mode {
			case MaildirMode:
				curview = MAILDIR_VIEW
			case MessageMode:
				refresh_message(view, g)
				curview = MESSAGE_VIEW
			}
			_, err := g.SetViewOnTop(curview)
			if err != nil {
				return err
			}
			err = g.SetCurrentView(curview)
			if err != nil {
				return err
			}
			return nil
		}
	}

	maildir_move := func(dy int) func(g *gocui.Gui, v *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			view.cur_maildir_view.scroll(v, dy)
			return nil
		}
	}
	if err := g.SetKeybinding(MAILDIR_VIEW, gocui.KeyPgup, gocui.ModNone, maildir_move(-10)); err != nil {
		return err
	}
	if err := g.SetKeybinding(MAILDIR_VIEW, gocui.KeyPgdn, gocui.ModNone, maildir_move(10)); err != nil {
		return err
	}
	if err := g.SetKeybinding(MAILDIR_VIEW, gocui.KeyArrowDown, gocui.ModNone, maildir_move(1)); err != nil {
		return err
	}
	if err := g.SetKeybinding(MAILDIR_VIEW, 'j', gocui.ModNone, maildir_move(1)); err != nil {
		return err
	}
	if err := g.SetKeybinding(MAILDIR_VIEW, gocui.KeyArrowUp, gocui.ModNone, maildir_move(-1)); err != nil {
		return err
	}
	if err := g.SetKeybinding(MAILDIR_VIEW, 'k', gocui.ModNone, maildir_move(-1)); err != nil {
		return err
	}
	if err := g.SetKeybinding(MAILDIR_VIEW, 'q', gocui.ModNone, quit); err != nil {
		return err
	}
	if err := g.SetKeybinding(MAILDIR_VIEW, gocui.KeyEnter, gocui.ModNone, switch_to_mode(MessageMode)); err != nil {
		return err
	}
	if err := g.SetKeybinding(MESSAGE_VIEW, 'q', gocui.ModNone, switch_to_mode(MaildirMode)); err != nil {
		return err
	}
	if err := g.SetKeybinding(MESSAGE_VIEW, gocui.KeyPgup, gocui.ModNone, scrollView(-10)); err != nil {
		return err
	}
	if err := g.SetKeybinding(MESSAGE_VIEW, gocui.KeyPgdn, gocui.ModNone, scrollView(10)); err != nil {
		return err
	}
	if err := g.SetKeybinding(MESSAGE_VIEW, gocui.KeySpace, gocui.ModNone, scrollView(10)); err != nil {
		return err
	}
	if err := g.SetKeybinding(MESSAGE_VIEW, 'j', gocui.ModNone, scrollView(1)); err != nil {
		return err
	}
	if err := g.SetKeybinding(MESSAGE_VIEW, 'k', gocui.ModNone, scrollView(-1)); err != nil {
		return err
	}
	if err := g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, quit); err != nil {
		return err
	}
	return nil
}

func get_layout(view *View) func(g *gocui.Gui) error {
	return func(g *gocui.Gui) error {
		maxX, maxY := g.Size()
		v, err := g.SetView(SIDE_VIEW, -1, -1, int(0.15*float32(maxX)), maxY-1)
		if err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			v.Frame = false
			w, h := v.Size()
			for i := 0; i < h; i++ {
				fmt.Fprintf(v, strings.Repeat(" ", w-1))
				fmt.Fprintln(v, "|")
			}
		}
		v, err = g.SetView(MESSAGE_VIEW, int(0.15*float32(maxX)), -1, maxX-1, maxY-1)
		if err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			v.Frame = false
		}
		v, err = g.SetView(MAILDIR_VIEW, int(0.15*float32(maxX)), -1, maxX-1, maxY-1)
		if err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			v.Frame = false
			v.Highlight = true
			view.cur_maildir_view.Draw(v)
			err = g.SetCurrentView(MAILDIR_VIEW)
			if err != nil {
				log.Panicln(err)
			}

		}
		v, err = g.SetView(SLIDER_VIEW, maxX-2, -1, maxX, maxY-1)
		if err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			v.Frame = false
			_, h := v.Size()
			slider_h := h * h / len(view.cur_maildir_view.md.messages)
			for i := 0; i < slider_h; i++ {
				fmt.Fprintln(v, "\u2588")
			}
		}
		v, err = g.SetView(STATUS_VIEW, -1, maxY-2, maxX, maxY)
		if err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			w, _ := v.Size()
			v.Frame = false
			format := fmt.Sprintf("\033[7m%%-%ds\033[0m", w)
			fmt.Fprintf(v, format, "Loaded all messages")
		}
		return nil
	}
}
func main() {
	usr, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}

	cfg, err := config.NewConfig(filepath.Join(usr.HomeDir, ".yamailrc"))
	if err != nil {
		log.Fatal(err)
	}
	if false {
		println(cfg)
	}

	md, err := LoadMaildir("md/cur")
	if err != nil {
		log.Fatal(err)
	}

	g := gocui.NewGui()
	if err := g.Init(); err != nil {
		log.Panicln(err)
	}
	defer g.Close()

	view := &View{}
	g.SetLayout(get_layout(view))
	mdv := &MaildirView{md: md}
	view.cur_maildir_view = mdv
	err = keybindings(view, g)
	if err != nil {
		log.Panicln(err)
	}

	if err := g.MainLoop(); err != nil && err != gocui.ErrQuit {
		log.Panicln(err)
	}

	return
}
