package main

import (
	"fmt"
	"sort"
	"io"
	"os"
	"io/ioutil"
	"bytes"
	"path/filepath"
	"time"

	"amua/util"

	"github.com/deweerdt/gocui"
)

type Maildir struct {
	path     string
	messages []*Message
}

type onMaildirChangeFn func(*knownMaildir)

func (km *knownMaildir) Start(onChange onMaildirChangeFn) {
	for {
		select {
		case <-km.stopMonitor:
			return
		case <-time.After(time.Second * 1):
			changed, _ := processNew(km.maildir, km.active)
			if changed {
				onChange(km)
			}
		}
	}
}
func (km *knownMaildir) Stop() {
	km.stopMonitor <- true
}

type readState struct {
	r       io.Reader // a reader we read the email from
	buffers []*bytes.Buffer
}


func processNew(md *Maildir, active bool) (bool, error) {
	curdir := filepath.Join(md.path, "cur")
	newdir := filepath.Join(md.path, "new")
	fis, err := ioutil.ReadDir(newdir)
	changed := false
	if err != nil {
		return false, err
	}
	for _, fi := range fis {
		oldName := fi.Name()
		newName := fmt.Sprintf("%s:2,", oldName)
		err := os.Rename(filepath.Join(newdir, oldName), filepath.Join(curdir, newName))
		if err != nil {
			return false, err
		}
		if active {
			m, err := LoadMessage(filepath.Join(curdir, newName))
			if err != nil {
				return false, err
			}
			md.messages = append(md.messages, m)
		} else {
			md.messages = append(md.messages, &Message{path: filepath.Join(curdir, newName)})
		}
		changed = true
	}
	return changed, nil
}

func LoadMaildir(mdPath string, active bool) (*Maildir, error) {
	md := &Maildir{}
	md.path = mdPath
	curdir := filepath.Join(mdPath, "cur")
	fis, err := ioutil.ReadDir(curdir)
	if err != nil {
		return nil, err
	}
	msgs := make([]*Message, len(fis))
	for i, fi := range fis {
		if active {
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
	_, err = processNew(md, active)
	if err != nil {
		return nil, err
	}

	return md, nil
}

func (md *Maildir) SortByDate() {
	sort.Sort(ByDate(md.messages))
}

type MaildirView struct {
	curTop int
	cur     int
	md      *Maildir
}

func (mv *MaildirView) Draw(v *gocui.View) error {
	v.Clear()
	w, h := v.Size()
	if h <= 1 {
		return fmt.Errorf("The screen is too small")
	}
	if w <= 20 {
		return fmt.Errorf("The screen is too small")
	}

	xo, _ := v.Origin()
	v.SetOrigin(xo, mv.curTop)
	xc, _ := v.Cursor()
	v.SetCursor(xc, mv.cur-mv.curTop)
	msgs := mv.md.messages
	flagsLen := 5
	indexLen := 6
	sizeLen := 5
	remW := w - indexLen - flagsLen - sizeLen + 2 /* two spaces */ + 2 /* two brackets around the size */
	fromRatio := 25
	subjRatio := 100 - fromRatio
	fromLen := (remW - 10) * fromRatio / 100.0
	subjLen := (remW - 10) * subjRatio / 100.0
	fmtString := fmt.Sprintf("%%-%dd%%-%ds%%-%ds [%%%ds] %%-%ds\n", indexLen, flagsLen, fromLen, sizeLen, subjLen)
	for i, m := range msgs {
		from := util.TruncateString(m.From, fromLen)
		subj := util.TruncateString(m.Subject, subjLen)
		flags := flagsToString(m.Flags)
		fmt.Fprintf(v, fmtString, i, flags, from, util.SiteToHuman(m.size), subj)

	}
	return nil
}

func (mv *MaildirView) scroll(v *gocui.View, incr int) {
	xo, yo := v.Origin()
	x, y := v.Cursor()
	_, h := v.Size()

	str := fmt.Sprintf("0: %d, ", incr)
	if mv.cur+incr > len(mv.md.messages)-1 {
		incr = len(mv.md.messages) - 1 - mv.cur
		str += fmt.Sprintf("1: %d, ", incr)
	}
	if mv.cur+incr < 0 {
		incr = 0 - mv.cur
		str += fmt.Sprintf("2: %d, ", incr)
	}
	mv.cur += incr
	y += incr
	if y >= h || y < 0 {
		mv.curTop += incr
		if mv.curTop < 0 {
			mv.curTop = 0
		}
		yo = yo + incr
		if yo < 0 {
			yo = 0
		}
		v.SetOrigin(xo, yo)
	}
	if y < 0 {
		y = 0
	}
	v.SetCursor(x, y)

	if false {
		xo, yo = v.Origin()
		x, y = v.Cursor()
		setStatus(fmt.Sprintf("%s origin=(%d), cursor=(%d), mv=(%d, %d)", str, yo, y, mv.cur, mv.curTop))
	}
}
