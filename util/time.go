package util

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Convert a duration specifier to seconds (e.g. 64 (seconds), 1:04, 0:1:04 etc.)
func ParseDurationSeconds(s string) (int, error) {
	var secs int
	sp := strings.Split(s, ":")
	if len(sp) < 1 || len(sp) > 3 {
		return 0, errors.New("invalid duration format")
	}
	magnitude := 1
	for i := len(sp) - 1; i >= 0; i-- {
		n, err := strconv.Atoi(sp[i])
		if n < 0 || err != nil {
			return 0, errors.New("invalid duration")
		}
		secs += n * magnitude
		magnitude *= 60
	}
	return secs, nil
}

func FormatDurationSeconds(s int) string {
	var h, m int
	h = s / 3600
	s -= h * 3600
	m = s / 60
	s -= m * 60

	var hs string
	if h > 0 {
		hs = fmt.Sprintf("%02d:", h)
	}
	return fmt.Sprintf("%s%02d:%02d", hs, m, s)
}
