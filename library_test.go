package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf16"
)

func testTracks() []track {
	tracks := make([]track, 24)
	for i := range tracks {
		tracks[i] = track{ID: fmt.Sprint(i), Title: fmt.Sprintf("Fixture song %02d", i), Artist: "Fixture artist", Year: 1960 + i*2, File: fmt.Sprintf("%02d.mp3", i)}
	}
	return tracks
}

func testMP3Frames() []byte {
	frame := make([]byte, 417)
	copy(frame, []byte{0xff, 0xfb, 0x90, 0xc4})
	return bytes.Repeat(frame, 80)
}

func synchsafe(size int) []byte {
	return []byte{byte(size >> 21 & 127), byte(size >> 14 & 127), byte(size >> 7 & 127), byte(size & 127)}
}

func taggedMP3(version byte, title, artist, year string) []byte {
	if version == 1 {
		tag := make([]byte, 128)
		copy(tag, "TAG")
		copy(tag[3:33], title)
		copy(tag[33:63], artist)
		copy(tag[93:97], year)
		return append(testMP3Frames(), tag...)
	}
	var frames []byte
	ids := []string{"TIT2", "TPE1", "TYER"}
	if version == 2 {
		ids = []string{"TT2", "TP1", "TYE"}
	} else if version == 4 {
		ids[2] = "TDRC"
	}
	for i, value := range []string{title, artist, year} {
		if value == "" {
			continue
		}
		payload := append([]byte{3}, []byte(value)...)
		if version != 4 {
			payload = []byte{1, 0xff, 0xfe}
			for _, r := range utf16.Encode([]rune(value)) {
				payload = binary.LittleEndian.AppendUint16(payload, r)
			}
		}
		frames = append(frames, ids[i]...)
		switch version {
		case 2:
			frames = append(frames, byte(len(payload)>>16), byte(len(payload)>>8), byte(len(payload)))
		case 3:
			frames = binary.BigEndian.AppendUint32(frames, uint32(len(payload)))
			frames = append(frames, 0, 0)
		case 4:
			frames = append(frames, synchsafe(len(payload))...)
			frames = append(frames, 0, 0)
		}
		frames = append(frames, payload...)
	}
	data := append([]byte{'I', 'D', '3', version, 0, 0}, synchsafe(len(frames))...)
	data = append(data, frames...)
	return append(data, testMP3Frames()...)
}

func writeTestAudio(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(dir, "audio", name)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLibraryUsesMP3Metadata(t *testing.T) {
	for _, version := range []byte{1, 2, 3, 4} {
		t.Run(fmt.Sprintf("ID3v%d", version), func(t *testing.T) {
			dir := t.TempDir()
			title, artist, year := "Tagged title", "Tagged artist", "1998"
			if version > 1 {
				title, artist = "Grüße aus 東京", "Björk"
			}
			if version == 4 {
				year = "1998-07-15"
			}
			original := taggedMP3(version, title, artist, year)
			path := writeTestAudio(t, dir, "Wrong artist - Wrong title (2025).MP3", original)
			if err := os.WriteFile(filepath.Join(dir, "library.json"), []byte("ignored invalid legacy JSON"), 0600); err != nil {
				t.Fatal(err)
			}
			for range 2 { // Reloads keep reading tags and do not modify the source.
				tracks, err := loadLibrary(dir)
				if err != nil || len(tracks) != 1 {
					t.Fatalf("load: %v, tracks: %+v", err, tracks)
				}
				got := tracks[0]
				if got.Title != title || got.Artist != artist || got.Year != 1998 || got.Start != 0 {
					t.Fatalf("did not use embedded metadata: %+v", got)
				}
				f, err := os.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				audio, err := mp3Audio(f)
				if err != nil {
					f.Close()
					t.Fatal(err)
				}
				cleaned, err := io.ReadAll(audio)
				f.Close()
				if err != nil || !bytes.Equal(cleaned, testMP3Frames()) {
					t.Fatal("audio contains metadata or lost audio bytes")
				}
			}
			stored, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(stored, original) {
				t.Fatal("original MP3 was changed")
			}
		})
	}
}

func TestLibrarySkipsInvalidSongs(t *testing.T) {
	dir := t.TempDir()
	tracks, err := loadLibrary(dir)
	if err != nil || len(tracks) != 0 {
		t.Fatalf("empty folder: %+v, %v", tracks, err)
	}
	valid := taggedMP3(3, "Title", "Artist", "2001")
	writeTestAudio(t, dir, "valid.mp3", valid)
	writeTestAudio(t, dir, "ignore.wav", valid)
	writeTestAudio(t, dir, "subfolder/ignore.mp3", valid)
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"no tags", testMP3Frames()},
		{"missing title", taggedMP3(3, "", "Artist", "2001")},
		{"missing artist", taggedMP3(3, "Title", "", "2001")},
		{"missing year", taggedMP3(3, "Title", "Artist", "")},
		{"invalid year", taggedMP3(3, "Title", "Artist", "unknown")},
		{"out of range year", taggedMP3(3, "Title", "Artist", "0001")},
		{"no audio", valid[:len(valid)-len(testMP3Frames())]},
		{"truncated tag", []byte{'I', 'D', '3', 3, 0, 0, 127, 127, 127, 127}},
		{"invalid tag size", []byte{'I', 'D', '3', 3, 0, 0, 128, 0, 0, 0}},
	} {
		path := writeTestAudio(t, dir, tc.name+" - Artist - Title (1998).mp3", tc.data)
		if _, err := loadTrack(path); err == nil {
			t.Errorf("accepted %s", tc.name)
		}
	}
	tracks, err = loadLibrary(dir)
	if err != nil || len(tracks) != 1 || tracks[0].File != "valid.mp3" {
		t.Fatalf("invalid songs entered library: %+v, %v", tracks, err)
	}
}

func TestFolderLibraryAndRemovedEndpoints(t *testing.T) {
	a, s := testApp(t)
	host := enterTest(t, s, "", "Host")
	apiTest(t, s, "/api/library", host.Token, nil, 404)
	apiTest(t, s, "/api/library", host.Token, map[string]string{"title": "Uploaded song"}, 404)
	var config map[string]any
	if err := json.Unmarshal(apiTest(t, s, "/api/config", "", nil, 200), &config); err != nil {
		t.Fatal(err)
	}
	if config["trackCount"] != float64(len(a.library)) || config["demoTrackCount"] != nil || config["customTrackCount"] != nil {
		t.Fatalf("incorrect library config: %+v", config)
	}
	a.mu.Lock()
	r := a.rooms[host.Code]
	p := r.findPlayer(host.PlayerID)
	err := a.applyAction(r, p, action{Type: "settings", Target: 7}, time.Now())
	if err == nil {
		err = a.applyAction(r, p, action{Type: "start"}, time.Now())
	}
	a.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if r.Target != 7 || r.Round.Track.File == "" || len(r.Deck) != len(a.library)-2 {
		t.Fatal("room did not use the folder library")
	}
	apiTest(t, s, "/data/audio/"+r.Round.Track.File, host.Token, nil, 404)
	req, _ := http.NewRequest("GET", s.URL+"/api/audio/"+r.Round.ID, nil)
	req.Header.Set("Authorization", "Bearer "+host.Token)
	req.Header.Set("Range", "bytes=0-15")
	res, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil || res.StatusCode != 206 || res.Header.Get("Content-Type") != "audio/mpeg" || !bytes.Equal(body, testMP3Frames()[:16]) {
		t.Fatal("range playback did not serve the correct audio bytes")
	}
	if strings.Contains(fmt.Sprint(res.Header), r.Round.Track.File) {
		t.Fatal("audio response exposed the source filename")
	}
}
