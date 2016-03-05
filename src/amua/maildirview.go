package main

import (
	"fmt"

	"amua/util"

	"github.com/deweerdt/gocui"
)

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
