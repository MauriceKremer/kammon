package web

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

func plural(n int, unit string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s ago", unit)
	}
	return fmt.Sprintf("%d %ss ago", n, unit)
}

// ago turns an age into words for screens, for example "2 minutes ago".
func ago(d time.Duration) string {
	switch {
	case d < 2*time.Second:
		return "just now"
	case d < time.Minute:
		return plural(int(d/time.Second), "second")
	case d < time.Hour:
		return plural(int(d/time.Minute), "minute")
	case d < 48*time.Hour:
		return plural(int(d/time.Hour), "hour")
	default:
		return plural(int(d/(24*time.Hour)), "day")
	}
}

// uptime formats a duration as "3d 14h 26m".
func uptime(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days, hours, minutes := int(d/(24*time.Hour)), int(d/time.Hour)%24, int(d/time.Minute)%60
	switch {
	case days > 0:
		return fmt.Sprintf("Uptime %dd %dh %dm", days, hours, minutes)
	case hours > 0:
		return fmt.Sprintf("Uptime %dh %dm", hours, minutes)
	default:
		return fmt.Sprintf("Uptime %dm", minutes)
	}
}

// runFor is an uptime without the word, for a line that already says what it is: "3d 4h 26m".
func runFor(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days, hours, minutes := int(d/(24*time.Hour)), int(d/time.Hour)%24, int(d/time.Minute)%60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}

// memSize writes a byte count for people: "512 KB", "21.4 MB", "1.25 GB".
func memSize(n uint64) string {
	const kb, mb = 1024, 1024 * 1024
	switch {
	case n < kb:
		return fmt.Sprintf("%d B", n)
	case n < mb:
		return fmt.Sprintf("%.0f KB", float64(n)/kb)
	case n < mb*1024:
		return fmt.Sprintf("%.1f MB", float64(n)/mb)
	default:
		return fmt.Sprintf("%.2f GB", float64(n)/(mb*1024))
	}
}

// thousands writes 1284 as "1,284".
func thousands(n int) string {
	s := strconv.Itoa(n)
	if n < 0 {
		return "-" + thousands(-n)
	}
	var out strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(r)
	}
	return out.String()
}

// orDash renders an empty string as a dash (MON-TR-8).
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
