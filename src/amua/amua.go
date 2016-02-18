package main

import (
	"bytes"
	"encoding/base64"
	"flag"
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
	if err != nil {
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

func LoadMaildir(md_path string, deep_load bool) (*Maildir, error) {
	md := &Maildir{}
	curdir := filepath.Join(md_path, "cur")
	fis, err := ioutil.ReadDir(curdir)
	if err != nil {
		return nil, err
	}
	msgs := make([]*Message, len(fis))
	for i, fi := range fis {
		if deep_load {
			m, err := LoadMessage(filepath.Join(curdir, fi.Name()))
			if err != nil {
				return nil, err
			}
			msgs[i] = m
		} else {
			msgs[i] = &Message{path: filepath.Join(curdir, fi.Name())}
		}

	}
	md.messages = msgs
	return md, nil
}

func (md *Maildir) SortByDate() {
	sort.Sort(ByDate(md.messages))
}

type Mode int

const (
	MaildirMode       Mode = 0
	MessageMode            = 1
	KnownMaildirsMode      = 2
	MaxMode                = 3
)

type Amua struct {
	mode             Mode
	cur_maildir_view *MaildirView
	cur_message_view *MessageView
	known_maildirs   []known_maildir
	curMaildir       int
}

func (amua *Amua) cur_message() *Message {
	return amua.cur_maildir_view.md.messages[amua.cur_maildir_view.cur]
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

func refresh_message(amua *Amua, g *gocui.Gui) error {
	v, err := g.View(MESSAGE_VIEW)
	if err != nil {
		return err
	}
	v.Clear()
	v.SetOrigin(0, 0)

	m := amua.cur_message()
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

func scrollMessageView(dy int) func(g *gocui.Gui, v *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		ox, oy := v.Origin()
		/* ignore errors */
		v.SetOrigin(ox, oy+dy)
		return nil
	}
}
func scrollSideView(amua *Amua, dy int) func(g *gocui.Gui, v *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		_, cy := v.Cursor()
		if cy+dy >= len(amua.known_maildirs) {
			return nil
		}
		if cy+dy < 0 {
			return nil
		}
		v.MoveCursor(0, dy, false)
		return nil
	}
}

func (amua *Amua) RefreshMaildir(v *gocui.View) error {
	md, err := LoadMaildir(amua.known_maildirs[amua.curMaildir].path, true)
	if err != nil {
		return err
	}
	amua.known_maildirs[amua.curMaildir].maildir = md
	mdv := &MaildirView{md: md}
	amua.cur_maildir_view = mdv
	v.SetCursor(0, 0)
	v.SetOrigin(0, 0)
	v.Clear()
	amua.cur_maildir_view.Draw(v)
	return nil
}

func selectNewMaildir(amua *Amua) func(g *gocui.Gui, v *gocui.View) error {
	return func(g *gocui.Gui, v *gocui.View) error {
		_, oy := v.Origin()
		_, cy := v.Cursor()
		selected := oy + cy
		if amua.curMaildir != selected {
			amua.curMaildir = selected
			mv, err := g.View(MAILDIR_VIEW)
			if err != nil {
				return err
			}
			err = amua.RefreshMaildir(mv)
			if err != nil {
				return err
			}
		}
		switchToMode(amua, g, MaildirMode)
		return nil
	}
}
func switchToMode(amua *Amua, g *gocui.Gui, mode Mode) error {
	amua.mode = mode
	curview := ""
	switch amua.mode {
	case MaildirMode:
		curview = MAILDIR_VIEW
	case MessageMode:
		refresh_message(amua, g)
		curview = MESSAGE_VIEW
	case KnownMaildirsMode:
		curview = SIDE_VIEW
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

func keybindings(amua *Amua, g *gocui.Gui) error {
	switchToModeInt := func(mode Mode) func(g *gocui.Gui, v *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			return switchToMode(amua, g, mode)
		}
	}

	maildir_move := func(dy int) func(g *gocui.Gui, v *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			amua.cur_maildir_view.scroll(v, dy)
			return nil
		}
	}
	maildir_all_down := func() func(g *gocui.Gui, v *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			dy := len(amua.cur_maildir_view.md.messages)
			amua.cur_maildir_view.scroll(v, dy)
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
	if err := g.SetKeybinding(MAILDIR_VIEW, 'G', gocui.ModNone, maildir_all_down()); err != nil {
		return err
	}
	if err := g.SetKeybinding(MAILDIR_VIEW, 'q', gocui.ModNone, quit); err != nil {
		return err
	}
	if err := g.SetKeybinding(MAILDIR_VIEW, 'c', gocui.ModNone, switchToModeInt(KnownMaildirsMode)); err != nil {
		return err
	}
	if err := g.SetKeybinding(MAILDIR_VIEW, gocui.KeyEnter, gocui.ModNone, switchToModeInt(MessageMode)); err != nil {
		return err
	}
	if err := g.SetKeybinding(MESSAGE_VIEW, 'q', gocui.ModNone, switchToModeInt(MaildirMode)); err != nil {
		return err
	}
	if err := g.SetKeybinding(MESSAGE_VIEW, gocui.KeyPgup, gocui.ModNone, scrollMessageView(-10)); err != nil {
		return err
	}
	if err := g.SetKeybinding(MESSAGE_VIEW, gocui.KeyPgdn, gocui.ModNone, scrollMessageView(10)); err != nil {
		return err
	}
	if err := g.SetKeybinding(MESSAGE_VIEW, gocui.KeySpace, gocui.ModNone, scrollMessageView(10)); err != nil {
		return err
	}
	if err := g.SetKeybinding(MESSAGE_VIEW, 'j', gocui.ModNone, scrollMessageView(1)); err != nil {
		return err
	}
	if err := g.SetKeybinding(MESSAGE_VIEW, 'k', gocui.ModNone, scrollMessageView(-1)); err != nil {
		return err
	}
	if err := g.SetKeybinding(SIDE_VIEW, 'j', gocui.ModNone, scrollSideView(amua, 1)); err != nil {
		return err
	}
	if err := g.SetKeybinding(SIDE_VIEW, gocui.KeyArrowDown, gocui.ModNone, scrollSideView(amua, 1)); err != nil {
		return err
	}
	if err := g.SetKeybinding(SIDE_VIEW, 'k', gocui.ModNone, scrollSideView(amua, -1)); err != nil {
		return err
	}
	if err := g.SetKeybinding(SIDE_VIEW, gocui.KeyArrowUp, gocui.ModNone, scrollSideView(amua, 1)); err != nil {
		return err
	}
	if err := g.SetKeybinding(SIDE_VIEW, gocui.KeyEnter, gocui.ModNone, selectNewMaildir(amua)); err != nil {
		return err
	}
	if err := g.SetKeybinding("", gocui.KeyCtrlC, gocui.ModNone, quit); err != nil {
		return err
	}
	return nil
}

func get_layout(amua *Amua) func(g *gocui.Gui) error {
	return func(g *gocui.Gui) error {
		maxX, maxY := g.Size()
		v, err := g.SetView(SIDE_VIEW, -1, -1, int(0.15*float32(maxX)), maxY-1)
		if err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			v.Frame = false
			v.Highlight = true
			w, h := v.Size()
			displayed := len(amua.known_maildirs)
			if len(amua.known_maildirs) > h {
				displayed = h
			}
			fillers := h - displayed
			space := 1
			for i := 0; i < displayed; i++ {
				nr_msgs := fmt.Sprintf("(%d)", len(amua.known_maildirs[i].maildir.messages))
				available_width := w - space - len(nr_msgs) - 3
				strfmt := fmt.Sprintf(" %%-%ds %s ", available_width, nr_msgs)
				fmt.Fprintf(v, strfmt, trunc(amua.known_maildirs[i].path, available_width))
				fmt.Fprintf(v, strings.Repeat(" ", space-1))
				fmt.Fprintln(v, "|")
			}
			for i := 0; i < fillers; i++ {
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
			amua.cur_maildir_view.Draw(v)
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
			slider_h := h * h / len(amua.cur_maildir_view.md.messages)
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

type known_maildir struct {
	maildir *Maildir // might be nil if not loaded
	path    string
}

func init_known_maildirs(maildirs []string) ([]known_maildir, error) {
	known_maildirs := make([]known_maildir, len(maildirs))
	for i, m := range maildirs {
		var err error
		var md *Maildir
		km := &known_maildirs[i]
		if i == 0 {
			md, err = LoadMaildir(m, true)
		} else {
			md, err = LoadMaildir(m, false)
		}
		if err != nil {
			return nil, err
		}
		km.maildir = md
		km.path = m
	}
	return known_maildirs, nil
}

func main() {
	var err error
	var cfg_file = flag.String("config", "", "the config file")
	flag.Parse()
	usr, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}

	if *cfg_file == "" {
		*cfg_file = filepath.Join(usr.HomeDir, ".amuarc")
	}
	cfg, err := config.NewConfig(*cfg_file)
	if err != nil {
		log.Fatal(err)
	}

	amua := &Amua{}
	amua.known_maildirs, err = init_known_maildirs(cfg.Maildirs)
	if err != nil {
		log.Fatal(err)
	}

	g := gocui.NewGui()
	if err := g.Init(); err != nil {
		log.Panicln(err)
	}
	defer g.Close()

	amua.curMaildir = 0
	md := amua.known_maildirs[amua.curMaildir].maildir
	g.SetLayout(get_layout(amua))
	mdv := &MaildirView{md: md}
	amua.cur_maildir_view = mdv
	err = keybindings(amua, g)
	if err != nil {
		log.Panicln(err)
	}

	if err := g.MainLoop(); err != nil && err != gocui.ErrQuit {
		log.Panicln(err)
	}

	return
}
