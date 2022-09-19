package util

import (
	"strings"
)

func CapitalizeFirst(s string) string {
	if len(s) == 0 {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// A StringTabulator aligns columns similarly to the tabulator
// character (but 100% rigorously).
type StringTabulator [][]string

func (t *StringTabulator) WriteRow(cols ...string) {
	*t = append(*t, cols)
}

func (t *StringTabulator) String() string {
	if t == nil {
		return ""
	}

	// Find which row has the most columns.
	longestRow := 0
	for _, row := range *t {
		if len(row) > longestRow {
			longestRow = len(row)
		}
	}

	// For each column, find the field with the longest string.
	longestField := make([]int, longestRow)
	for _, row := range *t {
		for i, col := range row {
			if len(col) > longestField[i] {
				longestField[i] = len(col)
			}
		}
	}

	// Add each line tabulated.
	var res strings.Builder
	for _, row := range *t {
		for i, col := range row {
			res.WriteString(col)
			res.WriteString(strings.Repeat(" ", longestField[i]-len(col)))
		}
		res.WriteString("\n")
	}

	// Return the result.
	return res.String()
}
