package textnorm

import "golang.org/x/text/unicode/norm"

func NFKC(s string) string { return norm.NFKC.String(s) }
