package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/dhowden/tag"
)

func loadLibrary(dir string) ([]track, error) {
	audioDir := filepath.Join(dir, "audio")
	if err := os.MkdirAll(audioDir, 0700); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(audioDir)
	if err != nil {
		return nil, err
	}
	tracks := []track{}
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !strings.EqualFold(filepath.Ext(entry.Name()), ".mp3") {
			continue
		}
		t, err := loadTrack(filepath.Join(audioDir, entry.Name()))
		if err != nil {
			log.Printf("Skipping %q: %v", entry.Name(), err)
			continue
		}
		tracks = append(tracks, t)
	}
	return tracks, nil
}

func loadTrack(path string) (track, error) {
	f, err := os.Open(path)
	if err != nil {
		return track{}, err
	}
	defer f.Close()
	metadata, err := tag.ReadFrom(f)
	if err != nil {
		return track{}, fmt.Errorf("read MP3 metadata: %w", err)
	}
	t := track{ID: newID(), File: filepath.Base(path), Title: cleanName(metadata.Title()), Artist: cleanName(metadata.Artist()), Year: metadata.Year()}
	if t.Title == "" || t.Artist == "" || t.Year < 1800 || t.Year > 2100 {
		return track{}, errors.New("MP3 metadata must include a title, artist and year between 1800 and 2100")
	}
	if _, err := mp3Audio(f); err != nil {
		return track{}, err
	}
	return t, nil
}

// Serve just the audio, leaving the source file and its answer-bearing ID3 tags intact.
func mp3Audio(f *os.File) (*io.SectionReader, error) {
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	start, end := int64(0), info.Size()
	var header [10]byte
	for end-start >= 10 {
		if _, err := f.ReadAt(header[:], start); err != nil {
			return nil, err
		}
		if string(header[:3]) != "ID3" {
			break
		}
		var size int64
		for _, b := range header[6:10] {
			if b&0x80 != 0 {
				return nil, errors.New("invalid MP3 metadata size")
			}
			size = size<<7 | int64(b)
		}
		start += 10 + size
		if header[3] == 4 && header[5]&0x10 != 0 {
			start += 10
		}
		if start > end {
			return nil, errors.New("incomplete MP3 metadata")
		}
	}
	if end-start >= 128 {
		var marker [3]byte
		if _, err := f.ReadAt(marker[:], end-128); err != nil {
			return nil, err
		}
		if string(marker[:]) == "TAG" {
			end -= 128
		}
	}
	audio := io.NewSectionReader(f, start, end-start)
	var probe [4096]byte
	n, err := audio.ReadAt(probe[:], 0)
	if err != nil && err != io.EOF {
		return nil, err
	}
	for i := 0; i+4 <= n; i++ {
		if probe[i] == 0xff && probe[i+1]&0xe0 == 0xe0 && probe[i+1]&0x06 != 0 && probe[i+1]&0x18 != 0x08 && probe[i+2]&0xf0 != 0xf0 && probe[i+2]&0x0c != 0x0c {
			return audio, nil
		}
	}
	return nil, errors.New("file does not contain MP3 audio")
}
