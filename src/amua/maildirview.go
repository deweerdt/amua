package main

import (
	"fmt"

	"amua/util"

	"github.com/deweerdt/gocui"
)

type MaildirView struct {
	cur_top int
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
	v.SetOrigin(xo, mv.cur_top)
	xc, _ := v.Cursor()
	v.SetCursor(xc, mv.cur-mv.cur_top)
	msgs := mv.md.messages
	flags_len := 5
	index_len := 6
	size_len := 5
	rem_w := w - index_len - flags_len - size_len + 2 /* two spaces */ + 2 /* two brackets around the size */
	from_ratio := 25
	subj_ratio := 100 - from_ratio
	from_len := (rem_w - 10) * from_ratio / 100.0
	subj_len := (rem_w - 10) * subj_ratio / 100.0
	fmt_string := fmt.Sprintf("%%-%dd%%-%ds%%-%ds [%%%ds] %%-%ds\n", index_len, flags_len, from_len, size_len, subj_len)
	for i, m := range msgs {
		from := util.TruncateString(m.From, from_len)
		subj := util.TruncateString(m.Subject, subj_len)
		flags := flagsToString(m.Flags)
		fmt.Fprintf(v, fmt_string, i, flags, from, util.SiteToHuman(m.size), subj)

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
		mv.cur_top += incr
		if mv.cur_top < 0 {
			mv.cur_top = 0
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
		setStatus(fmt.Sprintf("%s origin=(%d), cursor=(%d), mv=(%d, %d)", str, yo, y, mv.cur, mv.cur_top))
	}
}
