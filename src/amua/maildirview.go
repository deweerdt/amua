package main

import (
	"fmt"

	"github.com/deweerdt/gocui"
)

type MaildirView struct {
	cur int
	md  *Maildir
}

func (mv *MaildirView) Draw(v *gocui.View) {
	w, h := v.Size()
	if h <= 1 {
		return
	}

	msgs := mv.md.messages
	fmt_string := fmt.Sprintf("%%-%ds\n", w)
	for _, m := range msgs {
		fmt.Fprintf(v, fmt_string, m.Subject)
	}

}

func (mv *MaildirView) scroll(v *gocui.View, incr int) {
	xo, yo := v.Origin()
	x, y := v.Cursor()
	_, h := v.Size()

	if mv.cur+incr > len(mv.md.messages)-1 {
		incr = len(mv.md.messages) - 1 - mv.cur
	}
	if mv.cur+incr < 0 {
		incr = 0 - mv.cur
	}
	mv.cur += incr
	y += incr
	if y >= h || y <= 0 {
		v.SetOrigin(xo, yo+incr)
	}
	v.SetCursor(x, y)
}
