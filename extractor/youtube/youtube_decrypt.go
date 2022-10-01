package youtube

import (
	exutil "git.nobrain.org/r4/dischord/extractor/util"

	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

var (
	ErrDecryptGettingFunctionName = errors.New("error getting signature decryption function name")
	ErrDecryptGettingFunction     = errors.New("error getting signature decryption function")
	ErrDecryptGettingOpTable      = errors.New("error getting signature decryption operation table")
	ErrGettingBaseJs              = errors.New("unable to get base.js")
)

type decryptorOp struct {
	fn  func(a *string, b int)
	arg int
}

type decryptor struct {
	// base.js version ID, used for caching
	versionId string
	// The actual decryption algorithm can be split up into a list of known
	// operations
	ops []decryptorOp
}

func (d *decryptor) decrypt(input string) (string, error) {
	if err := updateDecryptor(d); err != nil {
		return "", err
	}

	s := input
	for _, op := range d.ops {
		op.fn(&s, op.arg)
	}
	return s, nil
}

type configData struct {
	PlayerJsUrl string `json:"PLAYER_JS_URL"`
}

func updateDecryptor(d *decryptor) error {
	prefix := "(function() {window.ytplayer={};\nytcfg.set("
	endStr := ");"
	// Get base.js URL
	var url string
	var funcErr error
	err := exutil.GetHTMLScriptFunc("https://www.youtube.com", false, func(code string) bool {
		if strings.HasPrefix(code, prefix) {
			// Cut out the JSON part
			code = code[len(prefix):]
			end := strings.Index(code, endStr)
			if end == -1 {
				funcErr = ErrGettingBaseJs
				return false
			}

			// Parse config data
			var data configData
			if err := json.Unmarshal([]byte(code[:end]), &data); err != nil {
				funcErr = ErrGettingBaseJs
				return false
			}

			url = "https://www.youtube.com" + data.PlayerJsUrl
			return false
		}
		return true
	})
	if err != nil {
		return err
	}
	if funcErr != nil {
		return err
	}

	// Get base.js version ID
	sp := strings.SplitN(strings.TrimPrefix(url, "/s/player/"), "/", 2)
	if len(sp) != 2 {
		return ErrGettingBaseJs
	}
	verId := sp[0]

	if d.versionId == verId {
		// Decryptor already up-to-date
		return nil
	}

	// Get base.js contents
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ErrGettingBaseJs
	}

	// Copy contents to buffer
	buf := new(strings.Builder)
	_, err = io.Copy(buf, resp.Body)
	if err != nil {
		return err
	}

	// Get decryption operations
	ops, err := getDecryptOps(buf.String())
	if err != nil {
		return err
	}

	d.versionId = verId
	d.ops = ops
	return nil
}

var decryptFunctionNameRegexp = regexp.MustCompile(`[a-zA-Z]*&&\([a-zA-Z]*=([a-zA-Z]*)\(decodeURIComponent\([a-zA-Z]*\)\),[a-zA-Z]*\.set\([a-zA-Z]*,encodeURIComponent\([a-zA-Z]*\)\)\)`)

func getDecryptFunction(baseJs string) (string, error) {
	idx := decryptFunctionNameRegexp.FindSubmatchIndex([]byte(baseJs))
	if len(idx) != 4 {
		return "", ErrDecryptGettingFunctionName
	}
	fnName := baseJs[idx[2]:idx[3]]

	startMatch := fnName + `=function(a){a=a.split("");`
	endMatch := `;return a.join("")};`
	start := strings.Index(baseJs, startMatch)
	if start == -1 {
		return "", ErrDecryptGettingFunction
	}
	fn := baseJs[start+len(startMatch):]
	end := strings.Index(fn, endMatch)
	if end == -1 {
		return "", ErrDecryptGettingFunction
	}
	return fn[:end], nil
}

func getDecryptOps(baseJs string) ([]decryptorOp, error) {
	// Extract main decryptor function JS
	decrFn, err := getDecryptFunction(baseJs)
	if err != nil {
		return nil, err
	}

	// Get decyptor operation JS
	var ops string
	{
		sp := strings.SplitN(decrFn, ".", 2)
		if len(sp) != 2 {
			return nil, ErrDecryptGettingOpTable
		}
		opsObjName := sp[0]

		startMatch := `var ` + opsObjName + `={`
		endMatch := `};`
		start := strings.Index(baseJs, startMatch)
		if start == -1 {
			return nil, ErrDecryptGettingOpTable
		}
		ops = baseJs[start+len(startMatch):]
		end := strings.Index(ops, endMatch)
		if end == -1 {
			return nil, ErrDecryptGettingOpTable
		}
		ops = ops[:end]
	}

	// Make a decryptor operation table that associates the operation
	// names with a specific action on an input string
	opTable := make(map[string]func(a *string, b int))
	{
		lns := strings.Split(ops, "\n")
		if len(lns) != 3 {
			return nil, ErrDecryptGettingOpTable
		}
		for _, ln := range lns {
			sp := strings.Split(ln, ":")
			if len(sp) != 2 {
				return nil, ErrDecryptGettingOpTable
			}
			name := sp[0]
			fn := sp[1]
			switch {
			case strings.HasPrefix(fn, `function(a){a.reverse()}`):
				opTable[name] = func(a *string, b int) {
					// Reverse a
					var res string
					for _, c := range *a {
						res = string(c) + res
					}
					*a = res
				}
			case strings.HasPrefix(fn, `function(a,b){var c=a[0];a[0]=a[b%a.length];a[b%a.length]=c}`):
				opTable[name] = func(a *string, b int) {
					// Swap a[0] and a[b % len(a)]
					c := []byte(*a)
					c[0], c[b%len(*a)] = c[b%len(*a)], c[0]
					*a = string(c)
				}
			case strings.HasPrefix(fn, `function(a,b){a.splice(0,b)}`):
				opTable[name] = func(a *string, b int) {
					// Slice off all elements of a up to a[b]
					*a = (*a)[b:]
				}
			}
		}
	}

	// Parse all operations in the main decryptor function and return them in
	// order
	var res []decryptorOp
	for _, fn := range strings.Split(decrFn, ";") {
		sp := strings.SplitN(fn, ".", 2)
		if len(sp) != 2 {
			return nil, ErrDecryptGettingOpTable
		}
		sp = strings.SplitN(sp[1], "(", 2)
		if len(sp) != 2 {
			return nil, ErrDecryptGettingOpTable
		}
		name := sp[0]
		argS := strings.TrimSuffix(strings.TrimPrefix(sp[1], "a,"), ")")
		arg, err := strconv.Atoi(argS)
		if err != nil {
			return nil, ErrDecryptGettingOpTable
		}
		callableOp, exists := opTable[name]
		if !exists {
			return nil, ErrDecryptGettingOpTable
		}
		res = append(res, decryptorOp{callableOp, arg})
	}
	return res, nil
}
