package core

import "strings"

type FilterResult int

const (
	FilterFalse FilterResult = 0
	FilterTrue  FilterResult = 1
	FilterMaybe FilterResult = 2
)

// FilterFn is the compiled form of a single include/exclude pattern.
type FilterFn func(isTree bool, segments []string) FilterResult

// compileFilter compiles a gitignore-style pattern. When returnMaybeOnPrefixMatch
// is true, tree-node prefix matches return FilterMaybe so recursion can continue
// into directories whose contents might match (used for --include patterns).
func compileFilter(pattern, errorHint string, returnMaybeOnPrefixMatch bool) (FilterFn, error) {
	if pattern == "" {
		return nil, userErrorf("Empty pattern: %s", errorHint)
	}
	if strings.Contains(pattern, "//") || pattern == "/" {
		return nil, userErrorf("Invalid pattern: %s", errorHint)
	}
	if pattern == "**" {
		return nil, userErrorf("To match anything, please use the pattern '*' instead: %s", errorHint)
	}
	if pattern == "**/" {
		return nil, userErrorf("To match any directory, please use the pattern '*/' instead: %s", errorHint)
	}
	treeOnly := false
	absolute := false
	if strings.HasPrefix(pattern, "/") {
		absolute = true
		pattern = pattern[1:]
	}
	if strings.HasSuffix(pattern, "/") {
		treeOnly = true
		pattern = pattern[:len(pattern)-1]
	}
	if strings.Contains(pattern, "/") {
		absolute = true
	}
	patternSegments := strings.Split(pattern, "/")
	if patternSegments[len(patternSegments)-1] == "**" {
		return nil, userErrorf("Ending a pattern with ** is the same as omitting it. please omit it: %s", errorHint)
	}

	// Split non-tip segments into runs separated by **.
	patternSegmentRuns := [][]string{{}}
	tipPattern := patternSegments[len(patternSegments)-1]
	for _, ps := range patternSegments[:len(patternSegments)-1] {
		if ps == "." || ps == ".." {
			return nil, userErrorf("Pattern cannot contain %q segments: %s", ps, errorHint)
		}
		if ps == "**" {
			patternSegmentRuns = append(patternSegmentRuns, []string{})
		} else {
			last := len(patternSegmentRuns) - 1
			patternSegmentRuns[last] = append(patternSegmentRuns[last], ps)
		}
	}
	totalNonTip := 0
	for _, run := range patternSegmentRuns {
		totalNonTip += len(run)
	}

	return func(isTree bool, segments []string) FilterResult {
		if treeOnly && !isTree {
			return FilterFalse
		}
		negative := FilterFalse
		if returnMaybeOnPrefixMatch && isTree {
			if !absolute {
				negative = FilterMaybe
			} else {
				run0 := patternSegmentRuns[0]
				prefix := len(run0)
				if len(segments) < prefix {
					prefix = len(segments)
				}
				for i := 0; i < prefix; i++ {
					if !fnmatch(segments[i], run0[i]) {
						return FilterFalse
					}
				}
				negative = FilterMaybe
			}
		}
		tip := segments[len(segments)-1]
		if !fnmatch(tip, tipPattern) {
			return negative
		}
		if !absolute {
			return FilterTrue
		}
		availableForwardShifts := len(segments) - 1 - totalNonTip
		nonTipSegs := segments[:len(segments)-1]
		if len(patternSegmentRuns) == 1 {
			if totalNonTip != len(nonTipSegs) {
				return negative
			}
		}
		patternSegmentAlignment := 0
		segmentsConsumed := 0
		for runIndex, run := range patternSegmentRuns {
			if runIndex == len(patternSegmentRuns)-1 {
				patternSegmentAlignment = availableForwardShifts
			}
			matched := false
			for patternSegmentAlignment <= availableForwardShifts {
				idx := patternSegmentAlignment + segmentsConsumed
				ok := true
				if idx+len(run) > len(nonTipSegs) {
					ok = false
				} else {
					for i, seg := range run {
						if !fnmatch(nonTipSegs[idx+i], seg) {
							ok = false
							break
						}
					}
				}
				if ok {
					segmentsConsumed += len(run)
					matched = true
					break
				}
				if runIndex == 0 {
					return negative
				}
				patternSegmentAlignment++
			}
			if !matched {
				return negative
			}
		}
		return FilterTrue
	}, nil
}
