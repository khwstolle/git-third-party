package core

// fnmatch implements case-sensitive shell-style wildcards (* ? [seq]) without regex.
func fnmatch(name, pattern string) bool {
	return fnmatchInner(name, pattern, 0, 0)
}

func fnmatchInner(name, pattern string, ni, pi int) bool {
	starNI, starPI := -1, -1
	for ni < len(name) {
		if pi < len(pattern) {
			pc := pattern[pi]
			switch pc {
			case '?':
				ni++
				pi++
				continue
			case '*':
				starPI = pi
				starNI = ni
				pi++
				continue
			case '[':
				end := pi + 1
				if end < len(pattern) && pattern[end] == '!' {
					end++
				}
				if end < len(pattern) && pattern[end] == ']' {
					end++
				}
				for end < len(pattern) && pattern[end] != ']' {
					end++
				}
				if end >= len(pattern) {
					// Unclosed [ is treated as a literal character.
					if name[ni] == pc {
						ni++
						pi++
						continue
					}
				} else {
					if charClassMatch(pattern[pi+1:end], name[ni]) {
						ni++
						pi = end + 1
						continue
					}
				}
			default:
				if name[ni] == pc {
					ni++
					pi++
					continue
				}
			}
		}
		if starPI >= 0 {
			pi = starPI + 1
			starNI++
			ni = starNI
			continue
		}
		return false
	}
	for pi < len(pattern) && pattern[pi] == '*' {
		pi++
	}
	return pi == len(pattern)
}

func charClassMatch(class string, c byte) bool {
	if len(class) == 0 {
		return false
	}
	negate := false
	if class[0] == '!' {
		negate = true
		class = class[1:]
	}
	matched := false
	for i := 0; i < len(class); i++ {
		if i+2 < len(class) && class[i+1] == '-' {
			lo := class[i]
			hi := class[i+2]
			if c >= lo && c <= hi {
				matched = true
			}
			i += 2
			continue
		}
		if class[i] == c {
			matched = true
		}
	}
	if negate {
		return !matched
	}
	return matched
}
