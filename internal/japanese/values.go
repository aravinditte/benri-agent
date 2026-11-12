package japanese

import "regexp"

var valueRe = regexp.MustCompile(`\d+(?:[.,]\d+)?(?:円|営業日|日以内|日|年間|年|か月|ヶ月|時間|%|％|点|件|回)|T\d{13}`)

// ExtractValues extracts policy-sensitive numeric values (amounts, periods,
// percentages, tracking numbers) from NFKC-normalized text.
func ExtractValues(text string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, v := range valueRe.FindAllString(Normalize(text), -1) {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}
