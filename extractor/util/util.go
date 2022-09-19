package util

import (
	"golang.org/x/net/html"

	"net/http"
)

// Retrieve JavaScript embedded in HTML
func GetHTMLScriptFunc(url string, readCodeLineByLine bool, codeFunc func(code string) bool) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	z := html.NewTokenizer(resp.Body)
	isScript := false

	for {
		tt := z.Next()

		switch tt {
		case html.ErrorToken:
			return z.Err()
		case html.TextToken:
			if codeFunc != nil && isScript {
				t := string(z.Text())
				if readCodeLineByLine {
					// NOTE: a bufio line scanner doesn't work (bufio.Scanner: token too long); maybe this is a bug
					// Iterate over each line in the script
					ls := 0 // line start
					le := 0 // line end
					for ls < len(t) {
						if le == len(t) || t[le] == '\n' {
							ln := t[ls:le]

							if !codeFunc(ln) {
								return nil
							}

							ls = le + 1
						}
						le++
					}
				} else {
					if !codeFunc(t) {
						return nil
					}
				}
			}
		case html.StartTagToken, html.EndTagToken:
			tn, _ := z.TagName()
			if string(tn) == "script" {
				isScript = tt == html.StartTagToken
			}
		}
	}
}
