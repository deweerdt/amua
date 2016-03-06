package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/mail"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"time"

	"amua/config"
	"amua/util"

	"github.com/deweerdt/gocui"
	"github.com/mitchellh/colorstring"
)

type Mode int

const (
	MaildirMode Mode = iota
	MessageMode
	MessageMimeMode
	KnownMaildirsMode
	CommandSearchMode
	CommandNewMailMode
	CommandMailModeTo
	CommandMailModeCc
	CommandMailModeBcc
	SendMailMode
	MaxMode
)

type Amua struct {
	mode           Mode           // the current mode the app is in
	prevMode       Mode           // the mode the app was in
	curMaildirView *MaildirView   // the current mailview
	knownMaildirs  []knownMaildir // list of loaded maildirs
	curMaildir     int            // index into knownMaildirs
	searchPattern  string         //currently searched pattern
	prompt         string         // current prompt: useful to know what to needs to be taken out of the view
	newMail        NewMail        // the mail currently beeing edited
}

func (amua *Amua) getMessage(idx int) *Message {
	return amua.curMaildirView.md.messages[idx]
}
func (amua *Amua) curMessage() *Message {
	return amua.getMessage(amua.curMaildirView.cur)
}

const (
	MAILDIR_VIEW   = "maildir"
	MESSAGE_VIEW   = "message"
	SLIDER_VIEW    = "slider"
	SIDE_VIEW      = "side"
	STATUS_VIEW    = "status"
	SEND_MAIL_VIEW = "send_mail"
	ERROR_VIEW     = "error"
)

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
		if cy+dy >= len(amua.knownMaildirs) {
			return nil
		}
		if cy+dy < 0 {
			return nil
		}
		v.MoveCursor(0, dy, false)
		return nil
	}
}

func (amua *Amua) applyCurMaildirChanges() error {
	md := amua.curMaildirView.md
	for _, m := range md.messages {
		if (m.Flags & Trashed) != 0 {
			err := os.Remove(m.path)
			if err != nil {
				panic(err)
			}
			continue
		}
		path := m.path
		i := strings.LastIndex(m.path, ":2,")
		if i != -1 {
			path = path[:i]
		}
		newPath := fmt.Sprintf("%s:2,%s", path, flagsToFile(m.Flags))
		if m.path != newPath {
			err := os.Rename(m.path, newPath)
			if err != nil {
				panic(err)
			}
		}
	}
	return nil
}
func (amua *Amua) RefreshMaildir(g *gocui.Gui, v *gocui.View) error {
	md, err := LoadMaildir(amua.knownMaildirs[amua.curMaildir].path, true)
	if err != nil {
		return err
	}
	amua.knownMaildirs[amua.curMaildir].maildir = md
	mdv := &MaildirView{md: md}
	amua.curMaildirView = mdv
	v.SetCursor(0, 0)
	v.SetOrigin(0, 0)
	err = amua.curMaildirView.Draw(v)
	if err != nil {
		fmt.Fprintf(v, err.Error())
	}
	drawSlider(amua, g)

	return nil
}

func drawKnownMaildirs(amua *Amua, g *gocui.Gui, v *gocui.View) error {
	v.Clear()
	v.Frame = false
	w, h := v.Size()
	displayed := len(amua.knownMaildirs)
	if len(amua.knownMaildirs) > h {
		displayed = h
	}
	fillers := h - displayed
	space := 1
	for i := 0; i < displayed; i++ {
		current := amua.knownMaildirs[i].maildir == amua.curMaildirView.md
		nrMsgs := fmt.Sprintf("(%d)", len(amua.knownMaildirs[i].maildir.messages))
		availableWidth := w - space - len(nrMsgs) - 3
		strfmt := fmt.Sprintf(" %%-%ds %s ", availableWidth, nrMsgs)
		str := fmt.Sprintf(strfmt, util.TruncateString(amua.knownMaildirs[i].path, availableWidth))
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
			amua.knownMaildirs[amua.curMaildir].active = false
			amua.curMaildir = selected
			mv, err := g.View(MAILDIR_VIEW)
			if err != nil {
				return err
			}
			err = amua.RefreshMaildir(g, mv)
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
	case CommandSearchMode:
		return STATUS_VIEW
	case CommandNewMailMode:
		return STATUS_VIEW
	case CommandMailModeTo:
		return STATUS_VIEW
	case CommandMailModeCc:
		return STATUS_VIEW
	case CommandMailModeBcc:
		return STATUS_VIEW
	case SendMailMode:
		return SEND_MAIL_VIEW
	}
	return ""
}
func (mode Mode) IsHighlighted() bool {
	return mode != MessageMode && mode != MessageMimeMode
}

const SEARCH_PROMPT = "Search: "
const TO_PROMPT = "To: "
const CC_PROMPT = "Cc: "
const BCC_PROMPT = "Bcc: "
const SUBJECT_PROMPT = "Subject: "

func getCommandEditor(amua *Amua) func(*gocui.View, gocui.Key, rune, gocui.Modifier) {
	return func(v *gocui.View, key gocui.Key, ch rune, mod gocui.Modifier) {
		prompt := amua.prompt
		// simpleEditor is used as the default gocui editor.
		switch {
		case ch != 0 && mod == 0:
			v.EditWrite(ch)
		case key == gocui.KeySpace:
			v.EditWrite(' ')
		case key == gocui.KeyBackspace || key == gocui.KeyBackspace2:
			xc, _ := v.Cursor()
			if xc <= len(prompt) {
				return
			}
			v.EditDelete(true)
		case key == gocui.KeyDelete:
			xc, _ := v.Cursor()
			if xc <= len(prompt) {
				return
			}
			v.EditDelete(false)
		case key == gocui.KeyArrowLeft:
			xc, _ := v.Cursor()
			if xc <= len(prompt) {
				return
			}
			v.MoveCursor(-1, 0, false)
		case key == gocui.KeyArrowRight:
			v.MoveCursor(1, 0, false)
		}
	}
}

func (amua *Amua) sendMailDraw(v *gocui.View) error {
	v.Clear()
	v.Frame = false
	v.Highlight = false
	v.Wrap = true
	v.SetOrigin(0, 0)

	tos := util.ConcatAddresses(amua.newMail.to)
	ccs := util.ConcatAddresses(amua.newMail.cc)
	bccs := util.ConcatAddresses(amua.newMail.bcc)
	fmt.Fprintf(v, "y: send, Ctrl+G: cancel, q: move to drafts, t: tos, c: ccs, b: bccs\n")
	fmt.Fprintf(v, "To: %s\n", tos)
	fmt.Fprintf(v, "Cc: %s\n", ccs)
	fmt.Fprintf(v, "Bcc: %s\n", bccs)
	fmt.Fprintf(v, "Subject: %s\n", amua.newMail.subject)
	fmt.Fprintf(v, "\n")
	return nil
}
func switchToMode(amua *Amua, g *gocui.Gui, mode Mode) error {
	/* highlight off */
	if amua.mode.IsHighlighted() {
		v := modeToView(g, amua.mode)
		v.Highlight = false
	}
	amua.prevMode = amua.mode
	amua.mode = mode
	curview := modeToViewStr(amua.mode)
	var err error
	switch amua.mode {
	case MessageMode:
		m := amua.curMessage()
		m.Flags |= Seen
		err = m.Draw(amua, g)
	case MessageMimeMode:
		m := amua.curMessage()
		err = (*MessageAsMimeTree)(m).Draw(amua, g)
	case MaildirMode:
		v, _ := g.View(curview)
		err = amua.curMaildirView.Draw(v)
	case SendMailMode:
		v, _ := g.View(curview)
		err = amua.sendMailDraw(v)
	case CommandNewMailMode:
		tos := util.ConcatAddresses(amua.newMail.to)
		amua.newMail.to = []*mail.Address{}
		displayPromptWithPrefill(TO_PROMPT, tos)
	case CommandMailModeTo:
		displayPromptWithPrefill(TO_PROMPT, util.ConcatAddresses(amua.newMail.to))
	case CommandMailModeCc:
		displayPromptWithPrefill(CC_PROMPT, util.ConcatAddresses(amua.newMail.cc))
	case CommandMailModeBcc:
		displayPromptWithPrefill(BCC_PROMPT, util.ConcatAddresses(amua.newMail.bcc))
	case CommandSearchMode:
		displayPrompt(SEARCH_PROMPT)
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

var displayError func(s string)

func keybindings(amua *Amua, g *gocui.Gui) error {
	switchToModeInt := func(mode Mode) func(g *gocui.Gui, v *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			return switchToMode(amua, g, mode)
		}
	}

	maildirMove := func(dy int) func(g *gocui.Gui, v *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			amua.curMaildirView.scroll(v, dy)
			drawSlider(amua, g)
			return nil
		}
	}
	maildirAllDown := func() func(g *gocui.Gui, v *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			dy := len(amua.curMaildirView.md.messages) - amua.curMaildirView.cur - 1
			amua.curMaildirView.scroll(v, dy)
			drawSlider(amua, g)
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
	search := func(forward bool) func(g *gocui.Gui, v *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			setStatus("Looking for: " + amua.searchPattern + " in " + amua.curMaildirView.md.path)
			found := false
			direction := 1
			if forward == false {
				direction = -1
			}
			for i := 0; i < len(amua.curMaildirView.md.messages); i++ {
				idx := ((direction * i) + amua.curMaildirView.cur + direction) % len(amua.curMaildirView.md.messages)
				if idx < 0 {
					idx = len(amua.curMaildirView.md.messages) + idx
				}
				if idx < 0 {
					panic(idx)
				}
				m := amua.getMessage(idx)
				if strings.Contains(m.Subject, amua.searchPattern) {
					setStatus("Found: " + amua.searchPattern + " in " + amua.curMaildirView.md.path)
					found = true
					amua.curMaildirView.curTop = idx
					amua.curMaildirView.cur = idx
					break
				}
			}
			if found {
				mv, err := g.View(MAILDIR_VIEW)
				if err != nil {
					return err
				}
				err = amua.curMaildirView.Draw(mv)
				if err != nil {
					setStatus(err.Error())
				}
			} else {
				setStatus(amua.searchPattern + " not found")
			}
			return nil
		}
	}
	enterSearch := func(forward bool) func(g *gocui.Gui, v *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			v.Rewind()
			spbuf, err := ioutil.ReadAll(v)
			if err != nil {
				return err
			}
			prompt := amua.prompt
			amua.searchPattern = strings.TrimSpace(string(spbuf[len(prompt):]))
			switchToMode(amua, g, MaildirMode)
			search(forward)(g, v)
			return nil
		}
	}
	cancelSearch := func(g *gocui.Gui, v *gocui.View) error {
		setStatus("")
		return switchToMode(amua, g, amua.prevMode)
	}
	setFlag := func(flag MessageFlags) func(g *gocui.Gui, v *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			m := amua.curMessage()
			m.Flags |= flag
			amua.curMaildirView.Draw(v)
			return nil
		}
	}
	unsetFlag := func(flag MessageFlags) func(g *gocui.Gui, v *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			m := amua.curMessage()
			m.Flags &= ^flag
			amua.curMaildirView.Draw(v)
			return nil
		}
	}
	toggleFlag := func(flag MessageFlags) func(g *gocui.Gui, v *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			m := amua.curMessage()
			if (m.Flags & flag) != 0 {
				m.Flags &= ^flag
			} else {
				m.Flags |= flag
			}
			amua.curMaildirView.Draw(v)
			return nil
		}
	}
	quit := func(g *gocui.Gui, v *gocui.View) error {
		amua.applyCurMaildirChanges()
		return gocui.ErrQuit
	}

	deleteMessage := setFlag(Trashed)
	undeleteMessage := unsetFlag(Trashed)
	unreadMessage := unsetFlag(Seen)
	readMessage := setFlag(Seen)
	toggleFlagged := toggleFlag(Flagged)

	syncMaildir := func(g *gocui.Gui, v *gocui.View) error {
		amua.applyCurMaildirChanges()
		amua.RefreshMaildir(g, v)
		v, _ = g.View(SIDE_VIEW)
		drawKnownMaildirs(amua, g, v)
		return nil
	}
	commandEnter := func(g *gocui.Gui, v *gocui.View) error {
		switch amua.mode {
		case CommandSearchMode:
			return enterSearch(true)(g, v)
		case CommandMailModeTo:
			var err error
			amua.newMail.to, err = mail.ParseAddressList(getPromptInput())
			if err != nil {
				displayError(err.Error())
				return nil
			}
			setStatus("")
			switchToMode(amua, g, SendMailMode)
		case CommandMailModeCc:
			var err error
			amua.newMail.cc, err = mail.ParseAddressList(getPromptInput())
			if err != nil {
				//flash error
				return nil
			}
			setStatus("")
			switchToMode(amua, g, SendMailMode)
		case CommandMailModeBcc:
			var err error
			amua.newMail.bcc, err = mail.ParseAddressList(getPromptInput())
			if err != nil {
				//flash error
				return nil
			}
			setStatus("")
			switchToMode(amua, g, SendMailMode)
		case CommandNewMailMode:
			if len(amua.newMail.to) == 0 {
				var err error
				amua.newMail.to, err = mail.ParseAddressList(getPromptInput())
				if err != nil {
					displayError(err.Error())
					return nil
				}
				displayPromptWithPrefill(SUBJECT_PROMPT, amua.newMail.subject)
				amua.newMail.subject = ""
			} else if amua.newMail.subject == "" {
				amua.newMail.subject = getPromptInput()
				/* Exec $EDITOR */
				tf, err := ioutil.TempFile("", "amuamail")
				if err != nil {
					log.Fatal(err)
				}
				defer os.Remove(tf.Name())
				tf.Close()
				err = ioutil.WriteFile(tf.Name(), amua.newMail.body, 0600)
				if err != nil {
					log.Fatal(err)
				}

				cmd := exec.Command("vim", tf.Name())
				cmd.Stdin = os.Stdin
				cmd.Stdout = os.Stdout
				if err := cmd.Run(); err != nil {
					log.Fatal(err)
				}
				amua.newMail.body, err = ioutil.ReadFile(tf.Name())
				if err != nil {
					log.Fatal(err.Error())
				}
				setStatus("")
				switchToMode(amua, g, SendMailMode)
				err = g.Sync()
				if err != nil {
					log.Fatal(err)
				}
			}
		}
		return nil
	}
	sendMail := func(g *gocui.Gui, v *gocui.View) error {
		err := send(&amua.newMail, cfg.SMTPConfig)
		if err != nil {
			setStatus(err.Error())
			return nil
		}
		setStatus("Sent to " + cfg.SMTPConfig.Host)
		switchToMode(amua, g, MaildirMode)
		amua.newMail = NewMail{}
		return nil
	}
	reply := func(group bool) func(g *gocui.Gui, v *gocui.View) error {
		return func(g *gocui.Gui, v *gocui.View) error {
			m := amua.curMessage()
			amua.newMail.to = buildTo(m)
			if group {
				amua.newMail.cc = buildCCs(m)
			}
			amua.newMail.subject = "Re: " + m.Subject
			buf, err := ioutil.ReadAll((*MessageAsText)(m))
			if err != nil {
				return err
			}
			buf = bytes.Replace(buf, []byte("\n"), []byte("\n> "), -1)
			replyHeader := fmt.Sprintf("On %s, %s wrote:\n> ", m.Date.Format("Mon Jan 2 15:04:05 -0700 MST 2006"), m.From)
			amua.newMail.body = append([]byte(replyHeader), buf...)
			switchToMode(amua, g, CommandNewMailMode)
			return nil
		}
	}
	replyMessage := reply(false)
	groupReplyMessage := reply(true)
	pipeMessage := func(g *gocui.Gui, v *gocui.View) error {
		m := amua.curMessage()
		cmd := exec.Command("vim", m.path)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		if err := cmd.Run(); err != nil {
			log.Fatal(err)
		}
		setStatus("")
		switchToMode(amua, g, MaildirMode)
		err := g.Sync()
		if err != nil {
			log.Fatal(err)
		}
		/* exec a command, pipe the message there */
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
			{'d', deleteMessage, false},
			{'u', undeleteMessage, false},
			{'G', maildirAllDown(), false},
			{'k', maildirMove(-1), false},
			{gocui.KeyArrowUp, maildirMove(-1), false},
			{'j', maildirMove(1), false},
			{'n', search(true), false},
			{'N', search(false), false},
			{'$', syncMaildir, false},
			{'F', toggleFlagged, false},
			{gocui.KeyCtrlR, readMessage, false},
			{gocui.KeyCtrlN, unreadMessage, false},
			{gocui.KeyArrowDown, maildirMove(1), false},
			{gocui.KeyCtrlF, maildirMove(10), false},
			{gocui.KeyPgdn, maildirMove(10), false},
			{gocui.KeyCtrlB, maildirMove(-10), false},
			{gocui.KeyPgup, maildirMove(-10), false},
			{'/', switchToModeInt(CommandSearchMode), false},
			{'m', switchToModeInt(CommandNewMailMode), false},
			{'r', replyMessage, false},
			{'g', groupReplyMessage, false},
			{'|', pipeMessage, false},
		},
		MESSAGE_VIEW: {
			{'q', switchToModeInt(MaildirMode), false},
			{'v', messageModeToggle, false},
			{gocui.KeyPgup, scrollMessageView(-10), false},
			{gocui.KeyPgdn, scrollMessageView(10), false},
			{gocui.KeySpace, scrollMessageView(10), false},
			{'j', scrollMessageView(1), false},
			{'k', scrollMessageView(-1), false},
			{'r', replyMessage, false},
			{'g', groupReplyMessage, false},
			{'|', pipeMessage, false},
		},
		SEND_MAIL_VIEW: {
			{'q', switchToModeInt(MaildirMode), false},
			{'t', switchToModeInt(CommandMailModeTo), false},
			{'c', switchToModeInt(CommandMailModeCc), false},
			{'b', switchToModeInt(CommandMailModeBcc), false},
			{'y', sendMail, false},
			{gocui.KeyCtrlG, switchToModeInt(MaildirMode), false},
		},
		STATUS_VIEW: {
			{gocui.KeyEnter, commandEnter, false},
			{gocui.KeyCtrlG, cancelSearch, false},
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

func drawSlider(amua *Amua, g *gocui.Gui) {
	v, err := g.View(SLIDER_VIEW)
	if err != nil {
		return
	}
	v.Clear()
	_, h := v.Size()
	sliderH := 1
	whites := h - 1
	if len(amua.curMaildirView.md.messages) > 0 {
		sliderH = h * h / len(amua.curMaildirView.md.messages)
		whites = amua.curMaildirView.curTop * h / len(amua.curMaildirView.md.messages)
	}
	if sliderH <= 0 {
		sliderH = 1
	}
	for i := 0; i < whites; i++ {
		fmt.Fprintln(v, " ")
	}
	for i := 0; i < sliderH; i++ {
		fmt.Fprintln(v, "\u2588")
	}
}

var setStatus func(s string)
var displayPrompt func(s string)
var displayPromptWithPrefill func(s string, prefill string)
var getPromptInput func() string

func getLayout(amua *Amua) func(g *gocui.Gui) error {
	return func(g *gocui.Gui) error {
		maxX, maxY := g.Size()
		v, err := g.SetView(SIDE_VIEW, -1, -1, int(0.15*float32(maxX)), maxY-1)
		if err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			drawKnownMaildirs(amua, g, v)
		}
		v, err = g.SetView(SEND_MAIL_VIEW, int(0.15*float32(maxX)), -1, maxX-1, maxY-1)
		if err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			amua.sendMailDraw(v)
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
			amua.curMaildirView.Draw(v)
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
			drawSlider(amua, g)
		}
		v, err = g.SetView(STATUS_VIEW, -1, maxY-2, maxX, maxY)
		if err != nil {
			if err != gocui.ErrUnknownView {
				return err
			}
			v.Frame = false
		}
		return nil
	}
}

type knownMaildir struct {
	maildir     *Maildir // might be nil if not loaded
	path        string
	stopMonitor chan bool
	active      bool //true if the maildir is actively being displayed, only one is at a given time
}

func initKnownMaildirs(maildirs []string, onChange onMaildirChangeFn) ([]knownMaildir, error) {
	knownMaildirs := make([]knownMaildir, len(maildirs))
	for i, m := range maildirs {
		var err error
		var md *Maildir
		km := &knownMaildirs[i]
		active := false
		if i == 0 {
			active = true
		}
		md, err = LoadMaildir(m, active)
		if err != nil {
			return nil, err
		}
		km.maildir = md
		km.path = m
		km.stopMonitor = make(chan bool)
		km.active = active
		go km.Start(onChange)
	}
	return knownMaildirs, nil
}

var cfg *config.Config

func main() {
	var err error
	var cfgFile = flag.String("config", "", "the config file")
	flag.Parse()
	usr, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}

	if *cfgFile == "" {
		defaultRc := filepath.Join(usr.HomeDir, ".amuarc")
		_, err := os.Stat(defaultRc)
		if err != nil {
			log.Fatalf("No config file provided, and can't open %s - ('%s'), exiting.", defaultRc, err.Error())
		}
		*cfgFile = defaultRc
	}
	cfg, err = config.NewConfig(*cfgFile)
	if err != nil {
		log.Fatal(err)
	}

	if len(cfg.AmuaConfig.Maildirs) == 0 {
		log.Fatal("No maildir defined in '%s', exiting.", *cfgFile)

	}
	amua := &Amua{}

	g := gocui.NewGui()
	if err := g.Init(); err != nil {
		log.Panicln(err)
	}
	g.Editor = gocui.EditorFunc(getCommandEditor(amua))
	defer g.Close()

	onchange := func(km *knownMaildir) {
		g.Execute(func(g *gocui.Gui) error {
			mv, err := g.View(MAILDIR_VIEW)
			if err != nil {
				return err
			}
			err = amua.RefreshMaildir(g, mv)
			if err != nil {
				return err
			}
			v, _ := g.View(SIDE_VIEW)
			drawKnownMaildirs(amua, g, v)
			return nil
		})
	}

	amua.knownMaildirs, err = initKnownMaildirs(cfg.AmuaConfig.Maildirs, onchange)
	if err != nil {
		log.Fatal(err)
	}
	amua.curMaildir = 0
	amua.prevMode = MaildirMode
	amua.mode = MaildirMode
	md := amua.knownMaildirs[amua.curMaildir].maildir
	g.SetLayout(getLayout(amua))
	mdv := &MaildirView{md: md}
	amua.curMaildirView = mdv
	err = keybindings(amua, g)
	if err != nil {
		log.Panicln(err)
	}

	displayPromptWithPrefill = func(s string, prefill string) {
		amua.prompt = s
		v, _ := g.View(STATUS_VIEW)
		v.Clear()
		v.SetOrigin(0, 0)
		v.SetCursor(0, 0)
		v.Editable = true
		fmt.Fprintf(v, amua.prompt)
		fmt.Fprintf(v, prefill)
		v.SetCursor(len(amua.prompt)+len(prefill), 0)
	}
	displayPrompt = func(s string) {
		displayPromptWithPrefill(s, "")
	}
	getPromptInput = func() string {
		v, err := g.View(STATUS_VIEW)
		if err != nil {
			return ""
		}
		v.Rewind()
		spbuf, err := ioutil.ReadAll(v)
		if err != nil {
			return ""
		}
		prompt := amua.prompt
		return strings.TrimSpace(string(spbuf[len(prompt):]))
	}
	setStatus = func(s string) {
		v, err := g.View(STATUS_VIEW)
		if err != nil {
			return
		}
		w, _ := v.Size()
		v.Clear()
		format := fmt.Sprintf("\033[7m%%-%ds\033[0m", w)
		fmt.Fprintf(v, format, s)
	}
	displayError = func(s string) {
		maxX, maxY := g.Size()
		if v, err := g.SetView(ERROR_VIEW, maxX/2-len(s), maxY/2, maxX/2+len(s), maxY/2+2); err != nil {
			if err != gocui.ErrUnknownView {
				return
			}
			v.Wrap = true
			v.Frame = true
			v.BgColor = gocui.ColorRed
			fmt.Fprintln(v, s)
		}
		curview := g.CurrentView()
		defer func() {
			go func() {
				g.Execute(func(g *gocui.Gui) error {
					time.Sleep(2e9)
					g.DeleteView(ERROR_VIEW)
					g.SetCurrentView(curview.Name())
					g.SetViewOnTop(curview.Name())
					return nil
				})
			}()
		}()
		if err := g.SetCurrentView(ERROR_VIEW); err != nil {
			return
		}
		_, err = g.SetViewOnTop(ERROR_VIEW)
		if err != nil {
			return
		}
		return
	}
	isMe = func(m *mail.Address) bool {
		for _, a := range cfg.AmuaConfig.Me {
			if m.Address == a {
				return true
			}
			if m.Address == fmt.Sprintf("<%s>", a) {
				return true
			}
		}
		return false
	}
	if err := g.MainLoop(); err != nil && err != gocui.ErrQuit {
		log.Panicln(err)
	}

	return
}
