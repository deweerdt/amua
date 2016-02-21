package main

import (
	"fmt"

	"github.com/deweerdt/gocui"
)

type MaildirView struct {
	cur int
	md  *Maildir
}

func size_to_human(size int64) string {
	fs := float64(size)
	const K = 1024
	const M = 1024 * K
	const G = 1024 * M
	const T = 1024 * G
	switch {
	case size > T/10:
		return fmt.Sprintf("%.1fT", fs/T)
	case size > G/10:
		return fmt.Sprintf("%.1fG", fs/G)
	case size > M/10:
		return fmt.Sprintf("%.1fM", fs/M)
	case size > K/10:
		return fmt.Sprintf("%.1fK", fs/K)
	default:
		return fmt.Sprintf("%d", size)
	}
}
func trunc(s string, max_len int) string {
	if len(s) > max_len {
		return s[:max_len]
	}
	return s

}
func (mv *MaildirView) Draw(v *gocui.View) error {
	w, h := v.Size()
	if h <= 1 {
		return fmt.Errorf("The screen is too small")
	}
	if w <= 20 {
		return fmt.Errorf("The screen is too small")
	}

	msgs := mv.md.messages
	index_len := 6
	size_len := 5
	rem_w := w - index_len - size_len + 3 /* three spaces */ + 2 /* two brackets around the size */
	from_ratio := 25
	subj_ratio := 100 - from_ratio
	from_len := (rem_w - 10) * from_ratio / 100.0
	subj_len := (rem_w - 10) * subj_ratio / 100.0
	fmt_string := fmt.Sprintf("%%-%dd %%-%ds [%%%ds] %%-%ds\n", index_len, from_len, size_len, subj_len)
	for i, m := range msgs {
		from := trunc(m.From, from_len)
		subj := trunc(m.Subject, subj_len)
		fmt.Fprintf(v, fmt_string, i, from, size_to_human(m.size), subj)
	}
	return nil
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
