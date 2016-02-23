package util

import (
	"fmt"
)

func SiteToHuman(size int64) string {
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
func TruncateString(s string, max_len int) string {
	if len(s) > max_len {
		return s[:max_len]
	}
	return s

}
