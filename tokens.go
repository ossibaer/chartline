package main

import (
	"errors"
	"slices"
	"sort"
	"time"
)

func (r *room) skipWithTokens(p *player, now time.Time) error {
	if r.Phase != "playing" || !r.isTurn(p) {
		return errors.New("Only the current team or solo player can spend tokens to skip.")
	}
	t := r.currentTimeline()
	if t.Tokens < 2 {
		return errors.New("Skipping a song costs two tokens.")
	}
	if len(r.Deck) == 0 {
		return errors.New("There are no replacement songs left. Keep your tokens and play this song.")
	}
	t.Tokens -= 2
	r.nextRound(now)
	return nil
}

func (r *room) stealDecided(id string) bool {
	if slices.Contains(r.Round.Passed, id) {
		return true
	}
	for _, steal := range r.Round.Steals {
		if steal.TimelineID == id {
			return true
		}
	}
	return false
}

func (r *room) steal(p *player, position int, pass bool, now time.Time) error {
	if r.Phase != "stealing" || !now.Before(r.Round.StealUntil) {
		return errors.New("The steal window is closed.")
	}
	t := r.playerTimeline(p)
	if t == nil || !slices.Contains(r.Round.StealEligible, t.ID) {
		return errors.New("Only opponents with a token can steal this song.")
	}
	if r.stealDecided(t.ID) {
		return errors.New("Your team has already stolen or passed on this song.")
	}
	if pass {
		r.Round.Passed = append(r.Round.Passed, t.ID)
	} else {
		if t.Tokens < 1 {
			return errors.New("Stealing costs one token.")
		}
		if position < 0 || position > len(r.currentTimeline().Cards) || position == r.Round.Locked.Position {
			return errors.New("Choose a different gap on the playing team's timeline.")
		}
		t.Tokens--
		r.Round.Steals = append(r.Round.Steals, stealView{t.ID, position})
	}
	r.resolveSteals(now)
	return nil
}

// The deadline prevents disconnected or undecided opponents from holding up play.
func (r *room) resolveSteals(now time.Time) bool {
	if r.Phase != "stealing" {
		return false
	}
	if now.Before(r.Round.StealUntil) {
		for _, id := range r.Round.StealEligible {
			if !r.stealDecided(id) {
				return false
			}
		}
	}
	r.resolveGuess()
	return true
}

func (t *timeline) addCard(c card) {
	position := sort.Search(len(t.Cards), func(i int) bool { return t.Cards[i].Year >= c.Year })
	t.Cards = append(t.Cards, card{})
	copy(t.Cards[position+1:], t.Cards[position:])
	t.Cards[position] = c
}

func (r *room) resolveGuess() {
	q, active := r.Round, r.currentTimeline()
	guess := q.Locked
	correct := validPlacement(active.Cards, q.Track.Year, guess.Position)
	result := &resultView{Card: q.Track.card(), Correct: correct, PlayerID: guess.PlayerID,
		TokenEarned: recognizedSong(q.Track, guess.Artist, guess.Title)}
	if result.TokenEarned {
		active.Tokens++
	}
	var winner *timeline
	if correct {
		// The active timeline always wins same-year ties against steals.
		winner = active
	} else {
		for _, steal := range q.Steals {
			if validPlacement(active.Cards, q.Track.Year, steal.Position) {
				for _, t := range r.Timelines {
					if t.ID == steal.TimelineID {
						winner = t
						break
					}
				}
				break
			}
		}
	}
	if winner != nil {
		winner.addCard(q.Track.card())
		result.AwardedTo = winner.ID
		if len(winner.Cards) >= r.Target {
			r.Winners = []string{winner.ID}
			r.FinishReason = "The timeline is complete."
		}
	}
	q.Result = result
	r.Phase = "reveal"
}
