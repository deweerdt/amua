package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	gomime "mime"
	"net/mail"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"amua/config"
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

type Maildir struct {
	path     string
	messages []*Message
}

type read_state struct {
	r       io.Reader // a reader we read the email from
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
func traverse(m *mime.MimePart) []*bytes.Buffer {
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
	case mime.MultipartMixed:
		ret = append(ret, traverse(m.Child)...)
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
				ret = append(ret, traverse(last)...)
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
		ret = append(ret, traverse(m.Next)...)
	}
	return ret
}

type MessageAsMimeTree Message

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
		m.rs = &read_state{}
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
		m.rs = &read_state{}
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

		m.rs.buffers = traverse(mtree)

		readers := make([]io.Reader, len(m.rs.buffers))
		for i, b := range m.rs.buffers {
			readers[i] = b
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
	MessageMimeMode        = 2
	KnownMaildirsMode      = 3
	MaxMode                = 4
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

func (m *Message) Draw(amua *Amua, g *gocui.Gui) error {
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
	err = amua.cur_maildir_view.Draw(v)
	if err != nil {
		fmt.Fprintf(v, err.Error())
	}

	return nil
}

func drawKnownMaildirs(amua *Amua, g *gocui.Gui, v *gocui.View) error {
	v.Clear()
	v.Frame = false
	w, h := v.Size()
	displayed := len(amua.known_maildirs)
	if len(amua.known_maildirs) > h {
		displayed = h
	}
	fillers := h - displayed
	space := 1
	for i := 0; i < displayed; i++ {
		current := amua.known_maildirs[i].maildir == amua.cur_maildir_view.md
		nr_msgs := fmt.Sprintf("(%d)", len(amua.known_maildirs[i].maildir.messages))
		available_width := w - space - len(nr_msgs) - 3
		strfmt := fmt.Sprintf(" %%-%ds %s ", available_width, nr_msgs)
		str := fmt.Sprintf(strfmt, util.TruncateString(amua.known_maildirs[i].path, available_width))
		if current {
			colorstring.Fprintf(v, "[bold]%s", str)
		} else {
			fmt.Fprint(v, str)
		}
		fmt.Fprintf(v, strings.Repeat(" ", space-1))
		fmt.Fprintln(v, "|")
	}
	for i := 0; i < fillers; i++ {
		fmt.Fprintf(v, strings.Repeat(" ", w-1))
		fmt.Fprintln(v, "|")
	}
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
		drawKnownMaildirs(amua, g, v)
		switchToMode(amua, g, MaildirMode)
		return nil
	}
}
func modeToView(g *gocui.Gui, mode Mode) *gocui.View {
	v, _ := g.View(modeToViewStr(mode))
	return v
}
func modeToViewStr(mode Mode) string {
	switch mode {
	case MaildirMode:
		return MAILDIR_VIEW
	case MessageMode:
		return MESSAGE_VIEW
	case MessageMimeMode:
		return MESSAGE_VIEW
	case KnownMaildirsMode:
		return SIDE_VIEW
	}
	return ""
}
func (mode Mode) IsHighlighted() bool {
	return mode != MessageMode && mode != MessageMimeMode
}
func switchToMode(amua *Amua, g *gocui.Gui, mode Mode) error {
	/* highlight off */
	if amua.mode.IsHighlighted() {
		v := modeToView(g, amua.mode)
		v.Highlight = false
	}
	amua.mode = mode
	curview := modeToViewStr(amua.mode)
	var err error
	switch amua.mode {
	case MessageMode:
		m := amua.cur_message()
		err = m.Draw(amua, g)
	case MessageMimeMode:
		m := amua.cur_message()
		err = (*MessageAsMimeTree)(m).Draw(amua, g)
	}

	if err != nil {
		v, _ := g.View(curview)
		if v != nil {
			fmt.Fprintf(v, err.Error())
		}
		/* we printed the error, fallback */
	}
	_, err = g.SetViewOnTop(curview)
	if err != nil {
		return err
	}
	err = g.SetCurrentView(curview)
	if err != nil {
		return err
	}
	/* highlight back */
	if amua.mode.IsHighlighted() {
		v := modeToView(g, amua.mode)
		v.Highlight = true
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
	messageModeToggle := func(g *gocui.Gui, v *gocui.View) error {
		switch amua.mode {
		case MessageMode:
			switchToMode(amua, g, MessageMimeMode)
		case MessageMimeMode:
			switchToMode(amua, g, MessageMode)
		}
		return nil
	}
	type keybinding struct {
		key interface{}
		fn  gocui.KeybindingHandler
		mod bool
	}
	bindings := map[string][]keybinding{
		MAILDIR_VIEW: {
			{gocui.KeyEnter, switchToModeInt(MessageMode), false},
			{'c', switchToModeInt(KnownMaildirsMode), false},
			{'v', switchToModeInt(MessageMimeMode), false},
			{'q', quit, false},
			{'G', maildir_all_down(), false},
			{'k', maildir_move(-1), false},
			{gocui.KeyArrowUp, maildir_move(-1), false},
			{'j', maildir_move(1), false},
			{gocui.KeyArrowDown, maildir_move(1), false},
			{gocui.KeyCtrlF, maildir_move(10), false},
			{gocui.KeyPgdn, maildir_move(10), false},
			{gocui.KeyCtrlP, maildir_move(-10), false},
			{gocui.KeyPgup, maildir_move(-10), false},
		},
		MESSAGE_VIEW: {
			{'q', switchToModeInt(MaildirMode), false},
			{'v', messageModeToggle, false},
			{gocui.KeyPgup, scrollMessageView(-10), false},
			{gocui.KeyPgdn, scrollMessageView(10), false},
			{gocui.KeySpace, scrollMessageView(10), false},
			{'j', scrollMessageView(1), false},
			{'k', scrollMessageView(-1), false},
		},
		SIDE_VIEW: {
			{'j', scrollSideView(amua, 1), false},
			{gocui.KeyArrowDown, scrollSideView(amua, 1), false},
			{'k', scrollSideView(amua, -1), false},
			{gocui.KeyArrowUp, scrollSideView(amua, 1), false},
			{gocui.KeyEnter, selectNewMaildir(amua), false},
		},
		"": {
			{gocui.KeyCtrlC, quit, false},
		},
	}

	for vn, binds := range bindings {
		for _, b := range binds {
			err := g.SetKeybinding(vn, b.key, gocui.ModNone, b.fn)
			if err != nil {
				return err
			}
		}
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
			drawKnownMaildirs(amua, g, v)
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
			if amua.mode == MaildirMode {
				v.Highlight = true
			}
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
