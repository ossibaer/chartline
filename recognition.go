package main

import (
	"regexp"
	"strings"
	"unicode"
)

var featureCredit = regexp.MustCompile(`(?i)[\s(\[]+(?:feat\.?|ft\.?|featuring)\s+`)
var artistSeparators = regexp.MustCompile(`[,;&]`)
var featuredArtists = regexp.MustCompile(`(?i)\s*(?:,|;|&)\s*|\s+(?:and|x)\s+`)
var titleParentheses = regexp.MustCompile(`\([^()]*\)`)

func normalizedGuess(value string) []rune {
	return []rune(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return unicode.ToLower(r)
	}, value))
}

// Similarity is 1 - edit distance / the longer normalized answer length.
// Insertions, deletions, and substitutions each count as one incorrect character.
func closeGuess(guess, answer string) bool {
	a, b := normalizedGuess(guess), normalizedGuess(answer)
	if len(a) == 0 || len(b) == 0 {
		return false
	}
	longest := max(len(a), len(b))
	if abs(len(a)-len(b))*5 > longest {
		return false
	}
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i, ac := range a {
		row := make([]int, len(b)+1)
		row[0] = i + 1
		for j, bc := range b {
			cost := 0
			if ac != bc {
				cost = 1
			}
			row[j+1] = min(row[j]+1, previous[j+1]+1, previous[j]+cost)
		}
		previous = row
	}
	return previous[len(b)]*5 <= longest
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// Feature credits may be stored in either the artist or title metadata.
func recognitionAnswers(t track) (string, []string) {
	artists := []string{t.Artist}
	title := t.Title
	for i, value := range []string{t.Artist, t.Title} {
		parts := featureCredit.Split(value, -1)
		if len(parts) == 1 {
			continue
		}
		main := strings.TrimSpace(parts[0])
		if i == 0 {
			artists = append(artists, main)
		} else {
			title = main
		}
		for _, credit := range parts[1:] {
			credit = strings.Trim(strings.TrimSpace(credit), "()[] ")
			artists = append(artists, credit)
			artists = append(artists, featuredArtists.Split(credit, -1)...)
		}
	}
	for _, credit := range artists {
		artists = append(artists, artistSeparators.Split(credit, -1)...)
	}
	return title, artists
}

func recognizedArtist(artist string, artists []string) bool {
	// Every submitted name must match a credit; order and separator do not matter.
	for _, name := range artistSeparators.Split(artist, -1) {
		matched := false
		for _, answer := range artists {
			if closeGuess(name, answer) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// Match any combination of optional title words at the same 80% threshold.
// Rows with the same answer length can be merged, avoiding an exponential list
// of expanded titles when several parenthesized phrases are present.
func closeTitleParts(guess string, parts [][]string) bool {
	distance, length := titlePartsDistance(guess, parts)
	return length > 0 && distance*5 <= length
}

func titlePartsDistance(guess string, parts [][]string) (int, int) {
	a := normalizedGuess(guess)
	if len(a) == 0 {
		return 0, 0
	}
	initial := make([]int, len(a)+1)
	for i := range initial {
		initial[i] = i
	}
	rows := map[int][]int{0: initial}
	for _, options := range parts {
		next := map[int][]int{}
		for length, previous := range rows {
			for _, option := range options {
				b := normalizedGuess(option)
				size := length + len(b)
				if size*4 > len(a)*5 {
					continue
				}
				row := previous
				for j, bc := range b {
					current := make([]int, len(a)+1)
					current[0] = length + j + 1
					for i, ac := range a {
						cost := 0
						if ac != bc {
							cost = 1
						}
						current[i+1] = min(current[i]+1, row[i+1]+1, row[i]+cost)
					}
					row = current
				}
				if existing, ok := next[size]; ok {
					for i := range existing {
						existing[i] = min(existing[i], row[i])
					}
				} else {
					next[size] = append([]int(nil), row...)
				}
			}
		}
		rows = next
	}
	bestDistance, bestLength := 0, 0
	for length, row := range rows {
		longest := max(len(a), length)
		if length > 0 && (bestLength == 0 || row[len(a)]*bestLength < bestDistance*longest) {
			bestDistance, bestLength = row[len(a)], longest
		}
	}
	return bestDistance, bestLength
}

func recognizedTitle(guess, title string) bool {
	title = strings.TrimSpace(title)
	groups := titleParentheses.FindAllStringIndex(title, -1)
	if len(groups) == 0 {
		return closeGuess(guess, title)
	}
	// The complete title, including its parentheses, always remains a valid answer.
	if string(normalizedGuess(guess)) == string(normalizedGuess(title)) {
		return true
	}
	last := groups[len(groups)-1]
	if last[1] == len(title) && closeGuess(guess, title[last[0]+1:last[1]-1]) {
		return true
	}
	if closeGuess(guess, titleParentheses.ReplaceAllString(title, "")) {
		return true
	}
	parts := [][]string{}
	optionalParts := [][]string{}
	position := 0
	for _, group := range groups {
		parts = append(parts, []string{title[position:group[0]]})
		phrase := title[group[0]+1 : group[1]-1]
		options := []string{title[group[0]:group[1]], phrase, ""}
		if group[1] != len(title) {
			optionalParts = append(optionalParts, options)
		}
		parts = append(parts, options)
		position = group[1]
	}
	parts = append(parts, []string{title[position:]})
	distance, length := titlePartsDistance(guess, parts)
	if length == 0 || distance*5 > length {
		return false
	}
	// Optional words alone do not qualify, including fuzzy guesses of them.
	// Prefer a valid title on ties, so its own 20% typo allowance still applies.
	optionalDistance, optionalLength := titlePartsDistance(guess, optionalParts)
	return optionalLength == 0 || distance*optionalLength <= optionalDistance*length
}

func recognizedSong(t track, artist, title string) bool {
	answerTitle, artists := recognitionAnswers(t)
	if !recognizedTitle(title, answerTitle) && !(answerTitle != t.Title && closeGuess(title, t.Title)) {
		return false
	}
	return recognizedArtist(artist, artists)
}
