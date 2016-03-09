package util

import (
	"fmt"
	"net/mail"
)

func AddressesToString(ads []*mail.Address) []string {
	ret := []string{}
	for _, a := range ads {
		ret = append(ret, a.String())
	}
	return ret
}
func ConcatAddresses(ads []*mail.Address) string {
	ret := ""
	for i, a := range ads {
		if i != 0 {
			ret += ", "
		}
		ret += a.String()
	}
	return ret
}
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
func TruncateString(s string, maxLen int) string {
	if len(s) > maxLen {
		return s[:maxLen]
	}
	return s

}
