package main

import (
	"regexp"
	"strings"
	"unicode"
)

var featureCredit = regexp.MustCompile(`(?i)[\s(\[]+(?:feat\.?|ft\.?|featuring)\s+`)
var featuredArtists = regexp.MustCompile(`(?i)\s*(?:,|;|&)\s*|\s+(?:and|x)\s+`)

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

// Explicit feature markers keep band names such as "Earth, Wind & Fire" intact.
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
	return title, artists
}

func recognizedSong(t track, artist, title string) bool {
	answerTitle, artists := recognitionAnswers(t)
	if !closeGuess(title, answerTitle) && !closeGuess(title, t.Title) {
		return false
	}
	for _, answer := range artists {
		if closeGuess(artist, answer) {
			return true
		}
	}
	return false
}
