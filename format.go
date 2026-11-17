package main

import "fmt"

// fmtTokens renders a token count compactly.
func fmtTokens(n int64) string {
	switch {
	case n >= 1e9:
		return fmt.Sprintf("%.2fB", float64(n)/1e9)
	case n >= 1e6:
		return fmt.Sprintf("%.2fM", float64(n)/1e6)
	case n >= 1e3:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	}
	return fmt.Sprintf("%d", n)
}

// fmtCost renders a USD amount.
func fmtCost(v float64) string {
	if v == 0 {
		return "$0.00"
	}
	return fmt.Sprintf("$%.2f", v)
}
