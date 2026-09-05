package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
)

var safeAudioFile = regexp.MustCompile(`^[a-f0-9]{32}\.mp3$`)

func loadLibrary(dir string) ([]track, error) {
	if err := os.MkdirAll(filepath.Join(dir, "audio"), 0700); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "library.json"))
	if errors.Is(err, os.ErrNotExist) {
		return []track{}, nil
	}
	if err != nil {
		return nil, err
	}
	var tracks []track
	if err := json.Unmarshal(data, &tracks); err != nil {
		return nil, fmt.Errorf("read library.json: %w", err)
	}
	for _, t := range tracks {
		if !safeAudioFile.MatchString(t.File) || t.Duration <= 0 || t.Duration > 60 || math.IsNaN(t.Start) || t.Start < 0 || t.Year < 1800 || t.Year > 2100 {
			return nil, fmt.Errorf("invalid library entry %q", t.Title)
		}
		if _, err := os.Stat(filepath.Join(dir, "audio", t.File)); err != nil {
			return nil, fmt.Errorf("missing audio for %q: %w", t.Title, err)
		}
	}
	return tracks, nil
}

func saveLibrary(dir string, tracks []track) error {
	data, err := json.MarshalIndent(tracks, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, "library.json")
	if err := os.WriteFile(path+".tmp", data, 0600); err != nil {
		return err
	}
	return os.Rename(path+".tmp", path)
}

// Strip the common ID3 tags so track names are not carried in the served MP3.
func cleanMP3(data []byte) ([]byte, error) {
	for len(data) >= 10 && string(data[:3]) == "ID3" {
		for _, b := range data[6:10] {
			if b&0x80 != 0 {
				return nil, errors.New("Invalid MP3 metadata.")
			}
		}
		size := 10 + int(data[6])<<21 + int(data[7])<<14 + int(data[8])<<7 + int(data[9])
		if data[3] == 4 && data[5]&0x10 != 0 {
			size += 10
		}
		if size > len(data) {
			return nil, errors.New("Incomplete MP3 file.")
		}
		data = data[size:]
	}
	if len(data) >= 128 && string(data[len(data)-128:len(data)-125]) == "TAG" {
		data = data[:len(data)-128]
	}
	for i := 0; i+4 < len(data) && i < 4096; i++ {
		if data[i] == 0xff && data[i+1]&0xe0 == 0xe0 && data[i+1]&0x06 != 0 && data[i+1]&0x18 != 0x08 && data[i+2]&0xf0 != 0xf0 && data[i+2]&0x0c != 0x0c {
			return data, nil
		}
	}
	return nil, errors.New("That file does not look like an MP3. Choose an MP3 audio file.")
}

func demoLibrary() []track {
	titles := []string{"Paper Satellites", "Velvet Morning", "Midnight Arcade", "Golden Hour", "Electric Avenue B", "Blue Bicycle", "After the Rain", "Little Comet", "Slow Motion", "Sunday Radio", "Neon Postcards", "Last Train Home", "Summer Static", "Moonlight Motel", "Soft Focus", "Dancing Shadows", "Silver Lining", "Apricot Skies", "Ocean Apartment", "Pocket Symphony", "Daydream Club", "Distant Lights", "Warm December", "Floating Gardens"}
	years := []int{1963, 1977, 1984, 1998, 2005, 2011, 1972, 2020, 1991, 1968, 1987, 2001, 2016, 1959, 1995, 1981, 2008, 2023, 1975, 1965, 2013, 1989, 2003, 2019}
	tracks := make([]track, len(titles))
	for i, title := range titles {
		tracks[i] = track{ID: fmt.Sprintf("demo-%d", i+1), Title: title, Artist: "Chartline Sessions", Year: years[i], Duration: 8, Demo: i + 1}
	}
	return tracks
}

// Original synthesized practice loops; the associated years are fictional.
func demoAudio(seed int) []byte {
	const rate = 22050
	const seconds = 8
	var b bytes.Buffer
	samples := rate * seconds
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36+samples*2))
	b.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(rate), uint32(rate * 2), uint16(2), uint16(16)} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(samples*2))
	scale := []int{0, 3, 5, 7, 10, 12, 15, 17}
	for i := 0; i < samples; i++ {
		t := float64(i) / rate
		beat := int(t * 4)
		within := math.Mod(t, 0.25)
		note := scale[(beat*(seed%3+1)+seed)%len(scale)]
		freq := 110 * math.Pow(2, float64(note+seed%7)/12)
		envelope := math.Min(within/0.01, 1) * math.Exp(-within*9)
		tone := (math.Sin(2*math.Pi*freq*t) + 0.2*math.Sin(4*math.Pi*freq*t)) * envelope
		kickTime := math.Mod(t, 0.5)
		kick := math.Sin(2*math.Pi*(55*kickTime+2*(1-math.Exp(-30*kickTime)))) * math.Exp(-kickTime*22)
		fade := math.Min(t*8, 1) * math.Min((seconds-t)*5, 1)
		value := int16((tone*0.23 + kick*0.17) * fade * 32767)
		_ = binary.Write(&b, binary.LittleEndian, value)
	}
	return b.Bytes()
}
