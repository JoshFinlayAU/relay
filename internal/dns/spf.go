package dns

import (
	"fmt"
	"strings"
)

// spfResult captures the outcome of walking an SPF tree.
type spfResult struct {
	found     bool // our include target appears in the tree
	lookups   int  // count of DNS-lookup-consuming mechanisms (RFC 7208 §4.6.4)
	overLimit bool // exceeded the 10-lookup limit
	detail    string
}

// lookupTXTFunc resolves TXT records for a name (one string per record).
type lookupTXTFunc func(name string) ([]string, error)

// spfRecord returns the single v=spf1 record among a name's TXT records, or "".
func spfRecord(txts []string) string {
	for _, t := range txts {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(t)), "v=spf1") {
			return strings.TrimSpace(t)
		}
	}
	return ""
}

// evaluateSPF walks the SPF tree for `domain`, following include: and redirect=
// to determine whether `includeTarget` is reachable, counting DNS-lookup
// mechanisms and flagging the RFC 7208 10-lookup limit.
func evaluateSPF(domain, includeTarget string, lookup lookupTXTFunc) spfResult {
	var res spfResult
	seen := map[string]bool{}

	var walk func(name string, depth int)
	walk = func(name string, depth int) {
		name = strings.TrimSuffix(strings.ToLower(name), ".")
		if seen[name] || depth > 10 {
			return
		}
		seen[name] = true

		txts, err := lookup(name)
		if err != nil {
			if res.detail == "" {
				res.detail = fmt.Sprintf("could not resolve SPF for %s: %v", name, err)
			}
			return
		}
		rec := spfRecord(txts)
		if rec == "" {
			if res.detail == "" {
				res.detail = fmt.Sprintf("no v=spf1 record at %s", name)
			}
			return
		}

		for _, tok := range strings.Fields(rec) {
			lower := strings.ToLower(tok)
			switch {
			case strings.HasPrefix(lower, "include:"):
				target := tok[len("include:"):]
				res.lookups++
				if strings.EqualFold(strings.TrimSuffix(target, "."), strings.TrimSuffix(includeTarget, ".")) {
					res.found = true
				}
				walk(target, depth+1)
			case strings.HasPrefix(lower, "redirect="):
				target := tok[len("redirect="):]
				res.lookups++
				if strings.EqualFold(strings.TrimSuffix(target, "."), strings.TrimSuffix(includeTarget, ".")) {
					res.found = true
				}
				walk(target, depth+1)
			case lower == "a" || strings.HasPrefix(lower, "a:") ||
				lower == "mx" || strings.HasPrefix(lower, "mx:") ||
				strings.HasPrefix(lower, "ptr") ||
				strings.HasPrefix(lower, "exists:"):
				res.lookups++
			}
		}
	}

	walk(domain, 0)
	if res.lookups > 10 {
		res.overLimit = true
	}
	return res
}
